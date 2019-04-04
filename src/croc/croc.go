package croc

import (
	"bytes"
	"crypto/elliptic"
	"encoding/json"
	"fmt"
	"io"
	"math"
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
	"github.com/schollz/croc/v5/src/utils"
	"github.com/schollz/pake"
	"github.com/schollz/progressbar/v2"
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

const BufferSize = 4096

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

	// steps involved in forming relationship
	Step1ChannelSecured       bool
	Step2FileInfoTransfered   bool
	Step3RecipientRequestFile bool
	Step4FileTransfer         bool
	Step5RecipientCheckFile   bool // TODO: Step5 should close files and reset things

	// send / receive information of all files
	FilesToTransfer           []FileInfo
	FilesToTransferCurrentNum int

	// send / receive information of current file
	CurrentFile       *os.File
	CurrentFileChunks []int64

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
	FolderRemote string    `json:"fr,omitempty"`
	FolderSource string    `json:"fs,omitempty"`
	Hash         []byte    `json:"h,omitempty"`
	Size         int64     `json:"s,omitempty"`
	ModTime      time.Time `json:"m,omitempty"`
	IsCompressed bool      `json:"c,omitempty"`
	IsEncrypted  bool      `json:"e,omitempty"`
}

type RemoteFileRequest struct {
	CurrentFileChunks         []int64
	FilesToTransferCurrentNum int
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
	PathToFiles      []string
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
		c.FilesToTransfer = make([]FileInfo, len(options.PathToFiles))
		for i, pathToFile := range options.PathToFiles {
			var fstats os.FileInfo
			var fullPath string
			fullPath, err = filepath.Abs(pathToFile)
			if err != nil {
				return
			}
			fullPath = filepath.Clean(fullPath)
			var folderName string
			folderName, _ = filepath.Split(fullPath)

			fstats, err = os.Stat(fullPath)
			if err != nil {
				return
			}
			c.FilesToTransfer[i] = FileInfo{
				Name:         fstats.Name(),
				FolderRemote: ".",
				FolderSource: folderName,
				Size:         fstats.Size(),
				ModTime:      fstats.ModTime(),
			}
			c.FilesToTransfer[i].Hash, err = utils.HashFile(fullPath)
			if err != nil {
				return
			}
			if options.KeepPathInRemote {
				var curFolder string
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
				c.FilesToTransfer[i].FolderRemote = strings.TrimPrefix(folderName, curFolder)
				c.FilesToTransfer[i].FolderRemote = filepath.ToSlash(c.FilesToTransfer[i].FolderRemote)
				c.FilesToTransfer[i].FolderRemote = strings.TrimPrefix(c.FilesToTransfer[i].FolderRemote, "/")
				if c.FilesToTransfer[i].FolderRemote == "" {
					c.FilesToTransfer[i].FolderRemote = "."
				}
			}
			log.Debugf("file %d info: %+v", i, c.FilesToTransfer[i])
		}
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
			int(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionSetBytes(int(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size)),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionThrottle(1/60*time.Second),
		)
		c.CurrentFile, err = os.Open(c.FilesToTransfer[c.FilesToTransferCurrentNum].Name)
		if err != nil {
			panic(err)
		}
		location := int64(0)
		for {
			buf := make([]byte, 4096*128)
			n, errRead := c.CurrentFile.Read(buf)
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
		err = json.Unmarshal(m.Bytes, &c.FilesToTransfer)
		if err != nil {
			return
		}
		c.log.Debug(c.FilesToTransfer)
		c.Step2FileInfoTransfered = true
	case "recipientready":
		var remoteFile RemoteFileRequest
		err = json.Unmarshal(m.Bytes, &remoteFile)
		if err != nil {
			return
		}
		c.FilesToTransferCurrentNum = remoteFile.FilesToTransferCurrentNum
		c.CurrentFileChunks = remoteFile.CurrentFileChunks
		c.Step3RecipientRequestFile = true
	case "chunk":
		var chunk Chunk
		err = json.Unmarshal(m.Bytes, &chunk)
		if err != nil {
			return
		}
		_, err = c.CurrentFile.WriteAt(chunk.Bytes, chunk.Location)
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
	case "finished-transfer":
		c.Step3RecipientRequestFile = false
		c.Step4FileTransfer = false
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type: "thanks",
		}.String()).Err()
	}
	if err != nil {
		return
	}
	err = c.updateState()

	return
}

func (c *Client) updateState() (err error) {
	if c.IsSender && c.Step1ChannelSecured && !c.Step2FileInfoTransfered {
		b, _ := json.Marshal(c.FilesToTransfer)
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type:  "fileinfo",
			Bytes: b,
		}.String()).Err()
		if err != nil {
			return
		}
		c.Step2FileInfoTransfered = true
	}
	if !c.IsSender && c.Step2FileInfoTransfered && !c.Step3RecipientRequestFile {
		// find the next file to transfer and send that number
		// if the files are the same size, then look for missing chunks
		finished := true
		for i, fileInfo := range c.FilesToTransfer {
			if i < c.FilesToTransferCurrentNum {
				continue
			}
			fileHash, errHash := utils.HashFile(path.Join(fileInfo.FolderRemote, fileInfo.Name))
			if errHash != nil || !bytes.Equal(fileHash, fileInfo.Hash) {
				finished = false
				c.FilesToTransferCurrentNum = i
				break
			}
			// TODO: print out something about this file already existing
		}
		if finished {
			// TODO: do the last finishing stuff
			log.Debug("finished")
			os.Exit(1)
		}

		// start initiating the process to receive a new file
		log.Debugf("working on file %d", c.FilesToTransferCurrentNum)

		// setup folder for new file
		if c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote != "." {
			err = os.MkdirAll(c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote, os.ModeDir)
			if err != nil {
				return
			}
		}

		pathToFile := path.Join(c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote, c.FilesToTransfer[c.FilesToTransferCurrentNum].Name)

		// check if file should be overwritten, or simply fixed with missing chunks
		overwrite := true
		fstats, errStats := os.Stat(pathToFile)
		if errStats == nil {
			if fstats.Size() == c.FilesToTransfer[c.FilesToTransferCurrentNum].Size {
				// just request missing chunks
				c.CurrentFileChunks = MissingChunks(pathToFile, fstats.Size(), 4096)
				log.Debugf("found %d missing chunks", len(c.CurrentFileChunks))
				overwrite = false
			}
		} else {
			c.CurrentFileChunks = []int64{}
		}
		if overwrite {
			c.CurrentFile, err = os.Create(pathToFile)
			if err != nil {
				return
			}
			err = c.CurrentFile.Truncate(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size)
		} else {
			c.CurrentFile, err = os.Open(pathToFile)
		}
		if err != nil {
			return
		}

		// recipient requests the file and chunks (if empty, then should receive all chunks)
		bRequest, _ := json.Marshal(RemoteFileRequest{
			CurrentFileChunks:         c.CurrentFileChunks,
			FilesToTransferCurrentNum: c.FilesToTransferCurrentNum,
		})
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type:  "recipientready",
			Bytes: bRequest,
		}.String()).Err()
		if err != nil {
			return
		}
		c.Step3RecipientRequestFile = true
		// start receiving data
		go func() {
			err = c.dataChannelReceive()
			if err != nil {
				panic(err)
			}
		}()
	}
	if c.IsSender && c.Step3RecipientRequestFile && !c.Step4FileTransfer {
		c.log.Debug("start sending data!")
		c.Step4FileTransfer = true
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
		for i := int64(0); i < c.FilesToTransfer[c.FilesToTransferCurrentNum].Size; i += 4096 {
			piecesToDo[i] = true
		}
		// Register message handling
		d.OnMessage(func(payload datachannel.Payload) {

			switch p := payload.(type) {
			case *datachannel.PayloadString:
				fmt.Printf("Message '%s' from DataChannel '%s' payload '%s'\n", p.PayloadType().String(), d.Label, string(p.Data))
				if bytes.Equal(p.Data, []byte("done")) {
					c.CurrentFile.Close()
					c.log.Debug(time.Since(timer))
					c.log.Debug("telling transfer is over")
					c.Step4FileTransfer = false
					c.Step3RecipientRequestFile = false
					err = c.redisdb.Publish(c.nameOutChannel, Message{
						Type: "finished-transfer",
					}.String()).Err()
					if err != nil {
						panic(err)
					}
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
				n, err = c.CurrentFile.WriteAt(chunk.Bytes, chunk.Location)
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

		pathToFile := path.Join(c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderSource, c.FilesToTransfer[c.FilesToTransferCurrentNum].Name)
		c.log.Debugf("sending '%s'", pathToFile)

		file, err := os.Open(pathToFile)
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

// MissingChunks returns the positions of missing chunks.
// If file doesn't exist, it returns an empty chunk list (all chunks).
// If the file size is not the same as requested, it returns an empty chunk list (all chunks).
func MissingChunks(fname string, fsize int64, chunkSize int) (chunks []int64) {
	fstat, err := os.Stat(fname)
	if fstat.Size() != fsize {
		return
	}

	f, err := os.Open(fname)
	if err != nil {
		return
	}
	defer f.Close()

	buffer := make([]byte, chunkSize)
	emptyBuffer := make([]byte, chunkSize)
	chunkNum := 0
	chunks = make([]int64, int64(math.Ceil(float64(fsize)/float64(chunkSize))))
	var currentLocation int64
	for {
		bytesread, err := f.Read(buffer)
		if err != nil {
			break
		}
		if bytes.Equal(buffer[:bytesread], emptyBuffer[:bytesread]) {
			chunks[chunkNum] = currentLocation
		}
		currentLocation += int64(bytesread)
	}
	return
}
