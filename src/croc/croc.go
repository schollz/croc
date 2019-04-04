package croc

import (
	"bytes"
	"crypto/elliptic"
	"encoding/base64"
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
	"github.com/schollz/croc/v5/src/utils"
	"github.com/schollz/pake"
	"github.com/schollz/progressbar/v2"
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

const BufferSize = 4096
const Channels = 2

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
	Step5CloseChannels        bool // TODO: Step5 should close files and reset things

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
	peerConnection [8]*webrtc.PeerConnection
	dataChannel    [8]*webrtc.DataChannel

	bar *progressbar.ProgressBar

	mutex *sync.Mutex
	quit  chan bool
}

type Message struct {
	Type    string `json:"t,omitempty"`
	Message string `json:"m,omitempty"`
	Bytes   []byte `json:"b,omitempty"`
	Num     int    `json:"n,omitempty"`
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

	c.mutex = &sync.Mutex{}
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
			if m.Type == "finished" {
				err = c.redisdb.Publish(c.nameOutChannel, Message{
					Type: "finished",
				}.String()).Err()
				return err
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
		c.bar = progressbar.NewOptions(
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
			c.bar.Add(n)
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
		offer := webrtc.SessionDescription{}
		err = Decode(m.Message, &offer)
		if err != nil {
			return
		}
		c.log.Debugf("got offer for %d:", m.Num)
		// Set the remote SessionDescription
		err = c.peerConnection[m.Num].SetRemoteDescription(offer)
		if err != nil {
			return
		}

		// Sets the LocalDescription, and starts our UDP listeners
		var answer webrtc.SessionDescription
		answer, err = c.peerConnection[m.Num].CreateAnswer(nil)
		if err != nil {
			return
		}

		// Output the answer in base64 so we can paste it in browser
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type:    "datachannel-answer",
			Message: Encode(answer),
			Num:     m.Num,
		}.String()).Err()
	case "datachannel-answer":
		c.log.Debug("got answer:", m.Message)
		var answer webrtc.SessionDescription

		err = Decode(m.Message, &answer)
		if err != nil {
			return
		}
		// Apply the answer as the remote description
		err = c.peerConnection[m.Num].SetRemoteDescription(answer)
	case "close-sender":
		c.peerConnection[m.Num].Close()
		c.peerConnection[m.Num] = nil
		c.dataChannel[m.Num].Close()
		c.dataChannel[m.Num] = nil
		// c.Step4FileTransfer = false
		// c.Step3RecipientRequestFile = false
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type: "close-recipient",
			Num:  m.Num,
		}.String()).Err()
	case "close-recipient":
		c.peerConnection[m.Num].Close()
		c.peerConnection[m.Num] = nil
		c.dataChannel[m.Num] = nil
		// c.Step4FileTransfer = false
		// c.Step3RecipientRequestFile = false
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
			err = c.redisdb.Publish(c.nameOutChannel, Message{
				Type: "finished",
			}.String()).Err()
			if err != nil {
				panic(err)
			}
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
				c.CurrentFileChunks = MissingChunks(pathToFile, fstats.Size(), BufferSize)
				log.Debugf("found %d missing chunks", len(c.CurrentFileChunks))
				overwrite = false
			}
		} else {
			c.CurrentFileChunks = []int64{}
		}
		if overwrite {
			os.Remove(pathToFile)
			c.CurrentFile, err = os.Create(pathToFile)
			if err != nil {
				return
			}
			err = c.CurrentFile.Truncate(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size)
		} else {
			c.CurrentFile, err = os.OpenFile(pathToFile, os.O_RDWR|os.O_CREATE, 0755)
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
		// Register message handling
		c.bar = progressbar.NewOptions64(
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionSetBytes64(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionThrottle(1/60*time.Second),
		)

		for inum := 0; inum < Channels; inum++ {
			go func(i int) {
				err = c.dataChannelReceive(i)
				if err != nil {
					panic(err)
				}
			}(inum)
		}
	}
	if c.IsSender && c.Step3RecipientRequestFile && !c.Step4FileTransfer {
		c.log.Debug("start sending data!")
		c.Step4FileTransfer = true
		c.bar = progressbar.NewOptions64(
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionSetBytes64(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionThrottle(1/60*time.Second),
		)
		for inum := 0; inum < Channels; inum++ {
			go func(i int) {
				err = c.dataChannelSend(i)
				if err != nil {
					panic(err)
				}
			}(inum)
		}
	}
	return
}

func (c *Client) dataChannelReceive(num int) (err error) {
	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Create a new RTCPeerConnection
	c.peerConnection[num], err = webrtc.NewPeerConnection(config)
	if err != nil {
		return
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	c.peerConnection[num].OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	// Register data channel creation handling
	c.peerConnection[num].OnDataChannel(func(d *webrtc.DataChannel) {

		// Register channel opening handling
		d.OnOpen(func() {
			c.log.Debugf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", d.Label(), d.ID())
		})

		startTime := false
		timer := time.Now()
		piecesToDo := make(map[int64]bool)
		for i := int64(0); i < c.FilesToTransfer[c.FilesToTransferCurrentNum].Size; i += BufferSize {
			piecesToDo[i] = true
		}
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			if bytes.Equal([]byte("done"), msg.Data) {
				c.log.Debug(time.Since(timer))
				c.log.Debug("telling transfer is over")
				err = c.redisdb.Publish(c.nameOutChannel, Message{
					Type: "close-sender",
					Num:  num,
				}.String()).Err()
				if err != nil {
					panic(err)
				}
				return
			}

			if !startTime {
				startTime = true
				timer = time.Now()
			}
			var chunk Chunk
			errM := json.Unmarshal(msg.Data, &chunk)
			if errM != nil {
				panic(errM)
			}
			var n int
			c.mutex.Lock()
			n, err = c.CurrentFile.WriteAt(chunk.Bytes, chunk.Location)
			c.log.Debugf("wrote %d bytes to %d (%d)", n, chunk.Location,num)
			c.mutex.Unlock()
			if err != nil {
				panic(err)
			}
			c.bar.Add(n)
		})

	})

	// Block forever
	return
}

func (c *Client) dataChannelSend(num int) (err error) {
	// Everything below is the pion-WebRTC API! Thanks for using it ❤️.

	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Create a new RTCPeerConnection
	c.peerConnection[num], err = webrtc.NewPeerConnection(config)
	if err != nil {
		return
	}

	// Create a datachannel with label 'data'
	c.dataChannel[num], err = c.peerConnection[num].CreateDataChannel("data", nil)
	if err != nil {
		return
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	c.peerConnection[num].OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	c.dataChannel[num].OnOpen(func() {
		fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", c.dataChannel[num].Label(), c.dataChannel[num].ID())
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
		chunkNum := 0.0
		for {
			bytesread, err := file.Read(buffer)
			if err != nil {
				if err != io.EOF {
					c.log.Debug(err)
				}
				break
			}

			if math.Mod(chunkNum, float64(Channels)) == float64(num) {
				mSend := Chunk{
					Bytes:    buffer[:bytesread],
					Location: location,
				}
				dataToSend, _ := json.Marshal(mSend)
				c.bar.Add(bytesread)
				err = c.dataChannel[num].Send(dataToSend)
				if err != nil {
					c.log.Debug("Could not send on data channel", err.Error())
					continue
				}
				time.Sleep(100 * time.Microsecond)
			}

			location += int64(bytesread)
			chunkNum += 1.0
		}
		c.log.Debug("sending done signal")
		err = c.dataChannel[num].Send([]byte("done"))
		if err != nil {
			c.log.Debug(err)
		}
		time.Sleep(1 * time.Second)
	})

	// Register text message handling
	c.dataChannel[num].OnMessage(func(msg webrtc.DataChannelMessage) {
		fmt.Printf("Message from DataChannel '%s': '%s'\n", c.dataChannel[num].Label(), string(msg.Data))
	})
	// Create an offer to send to the browser
	offer, err := c.peerConnection[num].CreateOffer(nil)
	if err != nil {
		return
	}

	// Sets the LocalDescription, and starts our UDP listeners
	err = c.peerConnection[num].SetLocalDescription(offer)
	if err != nil {
		return
	}

	// Output the offer in base64 so we can paste it in browser
	c.log.Debug("sending offer")
	err = c.redisdb.Publish(c.nameOutChannel, Message{
		Type:    "datachannel-offer",
		Message: Encode(offer),
		Num:     num,
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
	if chunkNum == 0 {
		chunks = []int64{}
	} else {
		chunks = chunks[:chunkNum]
	}
	return
}

// Encode encodes the input in base64
// It can optionally zip the input before encoding
func Encode(obj interface{}) string {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}

	return base64.StdEncoding.EncodeToString(b)
}

// Decode decodes the input from base64
// It can optionally unzip the input after decoding
func Decode(in string, obj interface{}) (err error) {
	b, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		return
	}

	err = json.Unmarshal(b, obj)
	return
}
