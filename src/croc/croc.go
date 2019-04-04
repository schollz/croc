package croc

import (
	"bytes"
	"crypto/elliptic"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/mattn/go-colorable"
	"github.com/pions/webrtc"
	"github.com/pions/webrtc/examples/util"
	"github.com/pions/webrtc/pkg/datachannel"
	"github.com/pions/webrtc/pkg/ice"
	"github.com/schollz/pake"
	"github.com/schollz/progressbar/v2"
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func init() {
	log.SetFormatter(&logrus.TextFormatter{ForceColors: true})
	log.SetOutput(colorable.NewColorableStdout())
	log.SetLevel(logrus.DebugLevel)
}

type Client struct {
	// basic setup
	redisdb      *redis.Client
	log          *logrus.Entry
	IsSender     bool
	SharedSecret string
	Pake         *pake.Pake
	Filename     string
	Folder       string

	// steps involved in forming relationship
	Step1ChannelSecured     bool
	Step2FileInfoTransfered bool
	Step3RecipientReady     bool
	Step4SendingData        bool

	// send / receive information
	f            *os.File
	FileInfo     FileInfo
	chunksToSend []int64

	// channel data
	incomingMessageChannel <-chan *redis.Message
	nameOutChannel         string
	nameInChannel          string

	// webrtc connections
	peerConnection *webrtc.RTCPeerConnection
	dataChannel    *webrtc.RTCDataChannel

	quit chan bool
}

type Message struct {
	Type    string `json:"t,omitempty"`
	Message string `json:"m,omitempty"`
	Bytes   []byte `json:"b,omitempty"`
}

type Chunk struct {
	Bytes    []byte `json:"b,omitempty"`
	Location int64  `json:"l,omitempty"`
}

type FileInfo struct {
	Name         string    `json:"n,omitempty"`
	Folder       string    `json:"f,omitempty"`
	Size         int64     `json:"s,omitempty"`
	ModTime      time.Time `json:"m,omitempty"`
	IsCompressed bool      `json:"c,omitempty"`
	IsEncrypted  bool      `json:"e,omitempty"`
}

func (m Message) String() string {
	b, _ := json.Marshal(m)
	return string(b)
}

// New establishes a new connection for transfering files between two instances.
func New(sender bool, sharedSecret string) (c *Client, err error) {
	c = new(Client)

	// setup basic info
	c.IsSender = sender
	c.SharedSecret = sharedSecret
	c.SharedSecret = sharedSecret
	if sender {
		c.nameOutChannel = c.SharedSecret + "2"
		c.nameInChannel = c.SharedSecret + "1"
	} else {
		c.nameOutChannel = c.SharedSecret + "1"
		c.nameInChannel = c.SharedSecret + "2"
	}

	// initialize redis for communication in establishing channel
	c.redisdb = redis.NewClient(&redis.Options{
		Addr:         "198.199.67.130:6372",
		Password:     "",
		DB:           4,
		WriteTimeout: 1 * time.Hour,
		ReadTimeout:  1 * time.Hour,
	})
	_, err = c.redisdb.Ping().Result()
	if err != nil {
		return
	}

	// setup channel for listening
	pubsub := c.redisdb.Subscribe(c.nameInChannel)
	_, err = pubsub.Receive()
	if err != nil {
		return
	}
	c.incomingMessageChannel = pubsub.Channel()

	// initialize pake
	if c.IsSender {
		c.Pake, err = pake.Init([]byte{1, 2, 3}, 1, elliptic.P521(), 1*time.Microsecond)
	} else {
		c.Pake, err = pake.Init([]byte{1, 2, 3}, 0, elliptic.P521(), 1*time.Microsecond)
	}
	if err != nil {
		return
	}

	// initialize logger
	c.log = log.WithFields(logrus.Fields{
		"is": "sender",
	})
	if !c.IsSender {
		c.log = log.WithFields(logrus.Fields{
			"is": "recipient",
		})
	}

	return
}

type TransferOptions struct {
	PathToFile       string
	KeepPathInRemote bool
}

// Send will send the specified file
func (c *Client) Send(options TransferOptions) (err error) {
	return c.transfer(options)
}

// Receive will receive a file
func (c *Client) Receive() (err error) {
	return c.transfer(TransferOptions{})
}

func (c *Client) transfer(options TransferOptions) (err error) {
	if c.IsSender {
		var fstats os.FileInfo
		fstats, err = os.Stat(path.Join(options.PathToFile))
		if err != nil {
			return
		}
		c.FileInfo = FileInfo{
			Name:    fstats.Name(),
			Folder:  ".",
			Size:    fstats.Size(),
			ModTime: fstats.ModTime(),
		}
		if options.KeepPathInRemote {
			var fullPath, curFolder string
			fullPath, err = filepath.Abs(options.PathToFile)
			if err != nil {
				return
			}
			fullPath = filepath.Clean(fullPath)
			folderName, _ := filepath.Split(fullPath)

			curFolder, err = os.Getwd()
			if err != nil {
				return
			}
			curFolder, err = filepath.Abs(curFolder)
			if err != nil {
				return
			}
			if !strings.HasPrefix(folderName, curFolder) {
				err = fmt.Errorf("remote directory must be relative to current")
				return
			}
			c.FileInfo.Folder = strings.TrimPrefix(folderName, curFolder)
			c.FileInfo.Folder = filepath.ToSlash(c.FileInfo.Folder)
			c.FileInfo.Folder = strings.TrimPrefix(c.FileInfo.Folder, "/")
			if c.FileInfo.Folder == "" {
				c.FileInfo.Folder = "."
			}
		}
		log.Debugf("file info: %+v", c.FileInfo)
	}
	// create channel for quitting
	// quit with c.quit <- true
	c.quit = make(chan bool)

	// if recipient, initialize with sending pake information
	c.log.Debug("ready")
	if !c.IsSender && !c.Step1ChannelSecured {
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type:  "pake",
			Bytes: c.Pake.Bytes(),
		}.String()).Err()
		if err != nil {
			return
		}
	}

	// listen for incoming messages and process them
	for {
		select {
		case <-c.quit:
			return
		case msg := <-c.incomingMessageChannel:
			var m Message
			err = json.Unmarshal([]byte(msg.Payload), &m)
			if err != nil {
				return
			}
			err = c.processMessage(m)
			if err != nil {
				return
			}
		default:
			time.Sleep(1 * time.Millisecond)
		}
	}
	return
}

func (c *Client) sendOverRedis() (err error) {
	go func() {
		bar := progressbar.NewOptions(
			int(c.FileInfo.Size),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionSetBytes(int(c.FileInfo.Size)),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionThrottle(1/60*time.Second),
		)
		c.f, err = os.Open(c.FileInfo.Name)
		if err != nil {
			panic(err)
		}
		location := int64(0)
		for {
			buf := make([]byte, 4096*128)
			n, errRead := c.f.Read(buf)
			bar.Add(n)
			chunk := Chunk{
				Bytes:    buf[:n],
				Location: location,
			}
			chunkB, _ := json.Marshal(chunk)
			err = c.redisdb.Publish(c.nameOutChannel, Message{
				Type:  "chunk",
				Bytes: chunkB,
			}.String()).Err()
			if err != nil {
				panic(err)
			}
			location += int64(n)
			if errRead == io.EOF {
				break
			}
			if errRead != nil {
				panic(errRead)
			}
		}
	}()
	return
}

func (c *Client) processMessage(m Message) (err error) {
	switch m.Type {
	case "pake":
		notVerified := !c.Pake.IsVerified()
		err = c.Pake.Update(m.Bytes)
		if err != nil {
			return
		}
		if (notVerified && c.Pake.IsVerified() && !c.IsSender) || !c.Pake.IsVerified() {
			err = c.redisdb.Publish(c.nameOutChannel, Message{
				Type:  "pake",
				Bytes: c.Pake.Bytes(),
			}.String()).Err()
		}
		if c.Pake.IsVerified() {
			c.log.Debug(c.Pake.SessionKey())
			c.Step1ChannelSecured = true
		}
	case "fileinfo":
		err = json.Unmarshal(m.Bytes, &c.FileInfo)
		if err != nil {
			return
		}
		c.log.Debug(c.FileInfo)
		if c.FileInfo.Folder != "." {
			err = os.MkdirAll(c.FileInfo.Folder, os.ModeDir)
			if err != nil {
				return
			}
		}
		c.f, err = os.Create(path.Join(c.FileInfo.Folder, c.FileInfo.Name))
		if err != nil {
			return
		}
		err = c.f.Truncate(c.FileInfo.Size)
		if err != nil {
			return
		}
		c.Step2FileInfoTransfered = true
	case "recipientready":
		c.Step3RecipientReady = true
	case "chunk":
		var chunk Chunk
		err = json.Unmarshal(m.Bytes, &chunk)
		if err != nil {
			return
		}
		_, err = c.f.WriteAt(chunk.Bytes, chunk.Location)
		c.log.Debug("writing chunk", chunk.Location)
	case "datachannel-offer":
		offer := util.Decode(m.Message)
		c.log.Debug("got offer:", m.Message)
		// Set the remote SessionDescription
		err = c.peerConnection.SetRemoteDescription(offer)
		if err != nil {
			return
		}

		// Sets the LocalDescription, and starts our UDP listeners
		var answer webrtc.RTCSessionDescription
		answer, err = c.peerConnection.CreateAnswer(nil)
		if err != nil {
			return
		}

		// Output the answer in base64 so we can paste it in browser
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type:    "datachannel-answer",
			Message: util.Encode(answer),
		}.String()).Err()
	case "datachannel-answer":
		answer := util.Decode(m.Message)
		// Apply the answer as the remote description
		err = c.peerConnection.SetRemoteDescription(answer)
	}
	if err != nil {
		return
	}
	err = c.updateState()

	return
}

func (c *Client) updateState() (err error) {
	if c.IsSender && c.Step1ChannelSecured && !c.Step2FileInfoTransfered {
		var fstats os.FileInfo
		fstats, err = os.Stat(path.Join(c.Folder, c.Filename))
		if err != nil {
			return
		}
		c.FileInfo = FileInfo{
			Name:    c.Filename,
			Folder:  c.Folder,
			Size:    fstats.Size(),
			ModTime: fstats.ModTime(),
		}
		b, _ := json.Marshal(c.FileInfo)
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type:  "fileinfo",
			Bytes: b,
		}.String()).Err()
		if err != nil {
			return
		}
		c.Step2FileInfoTransfered = true
	}
	if !c.IsSender && c.Step2FileInfoTransfered && !c.Step3RecipientReady {
		// TODO: recipient requests the chunk locations (if empty, then should receive all chunks)
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type: "recipientready",
		}.String()).Err()
		if err != nil {
			return
		}
		c.Step3RecipientReady = true
		// start receiving data
		go func() {
			err = c.dataChannelReceive()
			if err != nil {
				panic(err)
			}
		}()
	}
	if c.IsSender && c.Step3RecipientReady && !c.Step4SendingData {
		c.log.Debug("start sending data!")
		c.Step4SendingData = true
		go func() {
			err = c.dataChannelSend()
			if err != nil {
				panic(err)
			}
		}()
	}
	return
}

func (c *Client) dataChannelReceive() (err error) {
	// Prepare the configuration
	config := webrtc.RTCConfiguration{
		IceServers: []webrtc.RTCIceServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Create a new RTCPeerConnection
	c.peerConnection, err = webrtc.New(config)
	if err != nil {
		return
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	c.peerConnection.OnICEConnectionStateChange(func(connectionState ice.ConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	// Register data channel creation handling
	c.peerConnection.OnDataChannel(func(d *webrtc.RTCDataChannel) {
		fmt.Printf("New DataChannel %s %d\n", d.Label, d.ID)
		sendBytes := make(chan []byte, 1024)
		// Register channel opening handling
		d.OnOpen(func() {
			fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", d.Label, d.ID)
			for {
				data := <-sendBytes
				err := d.Send(datachannel.PayloadBinary{Data: data})
				if err != nil {
					c.log.Debug(err)
				}
			}
		})

		startTime := false
		timer := time.Now()
		var mutex = &sync.Mutex{}
		piecesToDo := make(map[int64]bool)
		for i := int64(0); i < c.FileInfo.Size; i += 4096 {
			piecesToDo[i] = true
		}
		// Register message handling
		d.OnMessage(func(payload datachannel.Payload) {

			switch p := payload.(type) {
			case *datachannel.PayloadString:
				fmt.Printf("Message '%s' from DataChannel '%s' payload '%s'\n", p.PayloadType().String(), d.Label, string(p.Data))
				if bytes.Equal(p.Data, []byte("done")) {
					c.f.Close()
					c.log.Debug(time.Since(timer))
				}
			case *datachannel.PayloadBinary:
				if !startTime {
					startTime = true
					timer = time.Now()
				}
				var chunk Chunk
				errM := json.Unmarshal(p.Data, &chunk)
				if errM != nil {
					panic(errM)
				}
				var n int
				mutex.Lock()
				n, err = c.f.WriteAt(chunk.Bytes, chunk.Location)
				mutex.Unlock()
				if err != nil {
					panic(err)
				}
				c.log.Debugf("wrote %d bytes to %d", n, chunk.Location)
				mutex.Lock()
				piecesToDo[chunk.Location] = false
				mutex.Unlock()
				go func() {
					numToDo := 0
					thingsToDo := make([]int64, len(piecesToDo))
					mutex.Lock()
					for k := range piecesToDo {
						if piecesToDo[k] {
							thingsToDo[numToDo] = k
							numToDo++
						}
					}
					mutex.Unlock()
					thingsToDo = thingsToDo[:numToDo]
					c.log.Debug("num to do: ", len(thingsToDo))
					if len(thingsToDo) < 10 {
						c.log.Debug(thingsToDo)
					}
				}()
			default:
				fmt.Printf("Message '%s' from DataChannel '%s' no payload \n", p.PayloadType().String(), d.Label)
			}
		})
	})

	// Block forever
	return
}

func (c *Client) dataChannelSend() (err error) {
	recievedBytes := make(chan []byte, 1024)
	// Everything below is the pion-WebRTC API! Thanks for using it ❤️.

	// Prepare the configuration
	config := webrtc.RTCConfiguration{
		IceServers: []webrtc.RTCIceServer{
			{
				URLs: []string{"stun:stun1.l.google.com:19305"},
			},
		},
	}

	// Create a new RTCPeerConnection
	c.peerConnection, err = webrtc.New(config)
	if err != nil {
		return
	}

	// Create a datachannel with label 'data'
	c.dataChannel, err = c.peerConnection.CreateDataChannel("data", nil)
	if err != nil {
		return
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	c.peerConnection.OnICEConnectionStateChange(func(connectionState ice.ConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	// Register channel opening handling
	c.dataChannel.OnOpen(func() {
		fmt.Printf("Data channel '%s'-'%d' open\n", c.dataChannel.Label, c.dataChannel.ID)
		time.Sleep(100 * time.Microsecond)

		c.log.Debug("sending file")
		const BufferSize = 4096
		file, err := os.Open("test.txt")
		if err != nil {
			c.log.Debug(err)
			return
		}
		defer file.Close()

		buffer := make([]byte, BufferSize)
		var location int64
		for {
			bytesread, err := file.Read(buffer)
			if err != nil {
				if err != io.EOF {
					c.log.Debug(err)
				}
				break
			}

			mSend := Chunk{
				Bytes:    buffer[:bytesread],
				Location: location,
			}
			dataToSend, _ := json.Marshal(mSend)

			c.log.Debugf("sending %d bytes at %d", bytesread, location)
			err = c.dataChannel.Send(datachannel.PayloadBinary{Data: dataToSend})
			if err != nil {
				c.log.Debug("Could not send on data channel", err.Error())
				continue
			}
			location += int64(bytesread)
			time.Sleep(100 * time.Microsecond)
		}
		c.log.Debug("sending done signal")
		err = c.dataChannel.Send(datachannel.PayloadString{Data: []byte("done")})
		if err != nil {
			c.log.Debug(err)
		}
	})

	// Register the OnMessage to handle incoming messages
	c.dataChannel.OnMessage(func(payload datachannel.Payload) {
		switch p := payload.(type) {
		case *datachannel.PayloadString:
			fmt.Printf("Message '%s' from DataChannel '%s' payload '%s'\n", p.PayloadType().String(), c.dataChannel.Label, string(p.Data))
		case *datachannel.PayloadBinary:
			fmt.Printf("Message '%s' from DataChannel '%s' payload '% 02x'\n", p.PayloadType().String(), c.dataChannel.Label, p.Data)
			recievedBytes <- p.Data
		default:
			fmt.Printf("Message '%s' from DataChannel '%s' no payload \n", p.PayloadType().String(), c.dataChannel.Label)
		}
	})

	// Create an offer to send to the browser
	offer, err := c.peerConnection.CreateOffer(nil)
	if err != nil {
		return
	}

	// Output the offer in base64 so we can paste it in browser
	c.log.Debug("sending offer")
	err = c.redisdb.Publish(c.nameOutChannel, Message{
		Type:    "datachannel-offer",
		Message: util.Encode(offer),
	}.String()).Err()
	if err != nil {
		return
	}

	return
}
