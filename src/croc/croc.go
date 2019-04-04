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
	peerConnection *webrtc.PeerConnection
	dataChannel    *webrtc.DataChannel

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
		offer := webrtc.SessionDescription{}
		err = Decode(m.Message, &offer)
		if err != nil {
			return
		}
		c.log.Debug("got offer:", m.Message)
		// Set the remote SessionDescription
		err = c.peerConnection.SetRemoteDescription(offer)
		if err != nil {
			return
		}

		// Sets the LocalDescription, and starts our UDP listeners
		var answer webrtc.SessionDescription
		answer, err = c.peerConnection.CreateAnswer(nil)
		if err != nil {
			return
		}

		// Output the answer in base64 so we can paste it in browser
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type:    "datachannel-answer",
			Message: Encode(answer),
		}.String()).Err()
	case "datachannel-answer":
		var answer webrtc.SessionDescription

		err = Decode(m.Message, &answer)
		if err != nil {
			return
		}
		// Apply the answer as the remote description
		err = c.peerConnection.SetRemoteDescription(answer)
	case "close-sender":
		c.peerConnection.Close()
		c.peerConnection = nil
		c.dataChannel = nil
		c.Step4FileTransfer = false
		c.Step3RecipientRequestFile = false
		err = c.redisdb.Publish(c.nameOutChannel, Message{
			Type: "close-recipient",
		}.String()).Err()
	case "close-recipient":
		c.peerConnection.Close()
		c.peerConnection = nil
		c.Step4FileTransfer = false
		c.Step3RecipientRequestFile = false
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
				c.CurrentFileChunks = MissingChunks(pathToFile, fstats.Size(), 4096)
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
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Create a new RTCPeerConnection
	c.peerConnection, err = webrtc.NewPeerConnection(config)
	if err != nil {
		return
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	c.peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	// Register data channel creation handling
	c.peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		fmt.Printf("New DataChannel %s %d\n", d.Label, d.ID)
	})

	sendBytes := make(chan []byte, 1024)
	// Register channel opening handling
	c.dataChannel.OnOpen(func() {
		log.Debugf("Data channel '%s'-'%d' open", c.dataChannel.Label, c.dataChannel.ID)
		for {
			data := <-sendBytes
			err := c.dataChannel.Send(data)
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
	bar := progressbar.NewOptions64(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetBytes64(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionThrottle(1/60*time.Second),
	)
	c.dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {

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
		mutex.Lock()
		n, err = c.CurrentFile.WriteAt(chunk.Bytes, chunk.Location)
		mutex.Unlock()
		if err != nil {
			panic(err)
		}
		// c.log.Debugf("wrote %d bytes to %d", n, chunk.Location)
		bar.Add(n)
	})

	// Block forever
	return
}

func (c *Client) dataChannelSend() (err error) {
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
	c.peerConnection, err = webrtc.NewPeerConnection(config)
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
	c.peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
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

		fstats, _ := file.Stat()
		bar := progressbar.NewOptions64(
			fstats.Size(),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionSetBytes64(fstats.Size()),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionThrottle(1/60*time.Second),
		)

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

			bar.Add(bytesread)
			err = c.dataChannel.Send(dataToSend)
			if err != nil {
				c.log.Debug("Could not send on data channel", err.Error())
				continue
			}
			location += int64(bytesread)
			time.Sleep(100 * time.Microsecond)
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Debug("Recovered in f", r)
				}
			}()
			for {
				c.log.Debug("sending done signal")
				if c.dataChannel == nil {
					return
				}
				err = c.dataChannel.Send([]byte("done"))
				if err != nil {
					c.log.Debug(err)
				}
				time.Sleep(1 * time.Second)
			}
		}()
	})

	// Register the OnMessage to handle incoming messages
	c.dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		fmt.Printf("Message from DataChannel '%s': '%s'\n", c.dataChannel.Label(), string(msg.Data))
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
		Message: Encode(offer),
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
