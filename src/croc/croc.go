package croc

import (
	"bytes"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	log "github.com/cihub/seelog"
	"github.com/denisbrodbeck/machineid"
	"github.com/pkg/errors"
	"github.com/schollz/croc/v6/src/comm"
	"github.com/schollz/croc/v6/src/compress"
	"github.com/schollz/croc/v6/src/crypt"
	"github.com/schollz/croc/v6/src/logger"
	"github.com/schollz/croc/v6/src/message"
	"github.com/schollz/croc/v6/src/models"
	"github.com/schollz/croc/v6/src/tcp"
	"github.com/schollz/croc/v6/src/utils"
	"github.com/schollz/pake"
	"github.com/schollz/peerdiscovery"
	"github.com/schollz/progressbar/v2"
	"github.com/schollz/spinner"
)

func init() {
	logger.SetLogLevel("debug")
}

func Debug(debug bool) {
	if debug {
		logger.SetLogLevel("debug")
	} else {
		logger.SetLogLevel("warn")
	}
}

type Options struct {
	IsSender     bool
	SharedSecret string
	Debug        bool
	RelayAddress string
	RelayPorts   []string
	Stdout       bool
	NoPrompt     bool
	DisableLocal bool
}

type Client struct {
	Options                         Options
	Pake                            *pake.Pake
	Key                             crypt.Encryption
	ExternalIP, ExternalIPConnected string

	// steps involved in forming relationship
	Step1ChannelSecured       bool
	Step2FileInfoTransfered   bool
	Step3RecipientRequestFile bool
	Step4FileTransfer         bool
	Step5CloseChannels        bool

	// send / receive information of all files
	FilesToTransfer           []FileInfo
	FilesToTransferCurrentNum int

	// send / receive information of current file
	CurrentFile           *os.File
	CurrentFileChunks     []int64
	TotalSent             int64
	TotalChunksTransfered int
	chunkMap              map[uint64]struct{}

	// tcp connections
	conn []*comm.Comm

	bar       *progressbar.ProgressBar
	spinner   *spinner.Spinner
	machineID string
	firstSend bool

	mutex *sync.Mutex
	quit  chan bool
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

type SenderInfo struct {
	MachineID       string
	FilesToTransfer []FileInfo
}

// New establishes a new connection for transfering files between two instances.
func New(ops Options) (c *Client, err error) {
	c = new(Client)

	// setup basic info
	c.Options = ops
	Debug(c.Options.Debug)
	log.Debugf("options: %+v", c.Options)

	c.conn = make([]*comm.Comm, 16)

	// use default key (no encryption, until PAKE succeeds)
	c.Key, err = crypt.New(nil, nil)
	if err != nil {
		return
	}

	// initialize pake
	if c.Options.IsSender {
		c.Pake, err = pake.Init([]byte(c.Options.SharedSecret), 1, elliptic.P521(), 1*time.Microsecond)
	} else {
		c.Pake, err = pake.Init([]byte(c.Options.SharedSecret), 0, elliptic.P521(), 1*time.Microsecond)
	}
	if err != nil {
		return
	}

	c.mutex = &sync.Mutex{}
	return
}

// TransferOptions for sending
type TransferOptions struct {
	PathToFiles      []string
	KeepPathInRemote bool
}

// Send will send the specified file
func (c *Client) Send(options TransferOptions) (err error) {
	c.FilesToTransfer = make([]FileInfo, len(options.PathToFiles))
	totalFilesSize := int64(0)
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
		totalFilesSize += fstats.Size()
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
	fname := fmt.Sprintf("%d files", len(c.FilesToTransfer))
	if len(c.FilesToTransfer) == 1 {
		fname = fmt.Sprintf("'%s'", c.FilesToTransfer[0].Name)
	}
	machID, macIDerr := machineid.ID()
	if macIDerr != nil {
		log.Error(macIDerr)
		return
	}
	if len(machID) > 6 {
		machID = machID[:6]
	}
	c.machineID = machID
	fmt.Fprintf(os.Stderr, "Sending %s (%s) from your machine, '%s'\n", fname, utils.ByteCountDecimal(totalFilesSize), machID)
	fmt.Fprintf(os.Stderr, "Code is: %s\nOn the other computer run\n\ncroc %s\n", c.Options.SharedSecret, c.Options.SharedSecret)
	// // c.spinner.Suffix = " waiting for recipient..."
	// c.spinner.Start()
	// create channel for quitting
	// connect to the relay for messaging
	errchan := make(chan error, 1)

	if !c.Options.DisableLocal {
		// setup the relay locally
		for _, port := range c.Options.RelayPorts {

			go func(portStr string) {
				debugString := "warn"
				if c.Options.Debug {
					debugString = "debug"
				}
				err = tcp.Run(debugString, portStr, strings.Join(c.Options.RelayPorts[1:], ","))
				if err != nil {
					panic(err)
				}
			}(port)
		}

		// look for peers first
		go func() {
			discoveries, err := peerdiscovery.Discover(peerdiscovery.Settings{
				Limit:     1,
				Payload:   []byte("9009"),
				Delay:     10 * time.Millisecond,
				TimeLimit: 30 * time.Second,
			})
			log.Debugf("discoveries: %+v", discoveries)

			if err == nil && len(discoveries) > 0 {
				log.Debug("using local server")
			}
		}()

		go func() {
			time.Sleep(500 * time.Millisecond)
			log.Debug("establishing connection")
			var banner string
			conn, banner, ipaddr, err := tcp.ConnectToTCPServer("localhost:"+c.Options.RelayPorts[0], c.Options.SharedSecret)
			log.Debugf("banner: %s", banner)
			if err != nil {
				err = errors.Wrap(err, fmt.Sprintf("could not connect to localhost:%s", c.Options.RelayPorts[0]))
				return
			}
			log.Debugf("connection established: %+v", conn)
			for {
				data, _ := conn.Receive()
				if bytes.Equal(data, []byte("handshake")) {
					break
				}
			}
			c.conn[0] = conn
			log.Debug("exchanged header message")
			c.Options.RelayAddress = "localhost"
			c.Options.RelayPorts = strings.Split(banner, ",")
			c.ExternalIP = ipaddr
			errchan <- c.transfer(options)
		}()
	}

	go func() {
		log.Debug("establishing connection to %s", c.Options.RelayAddress)
		var banner string
		conn, banner, ipaddr, err := tcp.ConnectToTCPServer(c.Options.RelayAddress, c.Options.SharedSecret)
		log.Debugf("banner: %s", banner)
		if err != nil {
			err = errors.Wrap(err, fmt.Sprintf("could not connect to %s", c.Options.RelayAddress))
			log.Debug(err)
			return
		}
		log.Debugf("connection established: %+v", conn)
		for {
			data, _ := conn.Receive()
			if bytes.Equal(data, []byte("handshake")) {
				break
			}
		}

		c.conn[0] = conn
		c.Options.RelayPorts = strings.Split(banner, ",")
		c.ExternalIP = ipaddr
		log.Debug("exchanged header message")
		errchan <- c.transfer(options)
	}()

	return <-errchan
}

// Receive will receive a file
func (c *Client) Receive() (err error) {
	fmt.Fprintf(os.Stderr, "connecting...")
	// recipient will look for peers first
	// and continue if it doesn't find any within 100 ms
	if !c.Options.DisableLocal {
		discoveries, err := peerdiscovery.Discover(peerdiscovery.Settings{
			Limit:     1,
			Payload:   []byte("ok"),
			Delay:     10 * time.Millisecond,
			TimeLimit: 100 * time.Millisecond,
		})
		if err == nil && len(discoveries) > 0 {
			log.Debug("switching to local")
			c.Options.RelayAddress = fmt.Sprintf("%s:%s", discoveries[0].Address, discoveries[0].Payload)
		}
		log.Debugf("discoveries: %+v", discoveries)
		log.Debug("establishing connection")
	}
	var banner string
	c.conn[0], banner, c.ExternalIP, err = tcp.ConnectToTCPServer(c.Options.RelayAddress, c.Options.SharedSecret)
	log.Debugf("banner: %s", banner)
	if err != nil {
		err = errors.Wrap(err, fmt.Sprintf("could not connect to %s", c.Options.RelayAddress))
		return
	}
	log.Debugf("connection established: %+v", c.conn[0])
	c.conn[0].Send([]byte("handshake"))
	c.Options.RelayPorts = strings.Split(banner, ",")
	log.Debug("exchanged header message")
	return c.transfer(TransferOptions{})
}

func (c *Client) transfer(options TransferOptions) (err error) {
	// connect to the server

	// quit with c.quit <- true
	c.quit = make(chan bool)

	// if recipient, initialize with sending pake information
	log.Debug("ready")
	if !c.Options.IsSender && !c.Step1ChannelSecured {
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:  "pake",
			Bytes: c.Pake.Bytes(),
		})
		if err != nil {
			return
		}
	}

	// listen for incoming messages and process them
	for {
		var data []byte
		var done bool
		data, err = c.conn[0].Receive()
		if err != nil {
			break
		}
		done, err = c.processMessage(data)
		if err != nil {
			break
		}
		if done {
			break
		}
	}

	if c.Options.Stdout && !c.Options.IsSender {
		pathToFile := path.Join(
			c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote,
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
		)
		os.Remove(pathToFile)
	}
	return
}

func (c *Client) processMessage(payload []byte) (done bool, err error) {
	m, err := message.Decode(c.Key, payload)
	if err != nil {
		return
	}

	switch m.Type {
	case "finished":
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type: "finished",
		})
		done = true
		return
	case "pake":
		log.Debug("received pake payload")
		// if // c.spinner.Suffix != " performing PAKE..." {
		// 	// c.spinner.Stop()
		// 	// c.spinner.Suffix = " performing PAKE..."
		// 	// c.spinner.Start()
		// }
		notVerified := !c.Pake.IsVerified()
		err = c.Pake.Update(m.Bytes)
		if err != nil {
			return
		}
		if (notVerified && c.Pake.IsVerified() && !c.Options.IsSender) || !c.Pake.IsVerified() {
			err = message.Send(c.conn[0], c.Key, message.Message{
				Type:  "pake",
				Bytes: c.Pake.Bytes(),
			})
		}
		if c.Pake.IsVerified() {
			if c.Options.IsSender {
				log.Debug("generating salt")
				salt := make([]byte, 8)
				rand.Read(salt)
				err = message.Send(c.conn[0], c.Key, message.Message{
					Type:    "salt",
					Bytes:   salt,
					Message: c.ExternalIP,
				})
				if err != nil {
					return
				}
			}

			// connects to the other ports of the server for transfer
			var wg sync.WaitGroup
			wg.Add(len(c.Options.RelayPorts))
			for i := 0; i < len(c.Options.RelayPorts); i++ {
				log.Debugf("port: [%s]", c.Options.RelayPorts[i])
				go func(j int) {
					defer wg.Done()
					server := fmt.Sprintf("%s:%s", strings.Split(c.Options.RelayAddress, ":")[0], c.Options.RelayPorts[j])
					log.Debugf("connecting to %s", server)
					c.conn[j+1], _, _, err = tcp.ConnectToTCPServer(
						server,
						fmt.Sprintf("%s-%d", utils.SHA256(c.Options.SharedSecret)[:7], j),
					)
					if err != nil {
						panic(err)
					}
					log.Debugf("connected to %s", server)
					if !c.Options.IsSender {
						go c.receiveData(j)
					}
				}(i)
			}
			wg.Wait()
		}
	case "salt":
		log.Debug("received salt")
		if !c.Options.IsSender {
			log.Debug("sending salt back")
			err = message.Send(c.conn[0], c.Key, message.Message{
				Type:    "salt",
				Bytes:   m.Bytes,
				Message: c.ExternalIP,
			})
		}
		log.Debugf("session key is verified, generating encryption with salt: %x", m.Bytes)
		key, err := c.Pake.SessionKey()
		if err != nil {
			return true, err
		}
		c.Key, err = crypt.New(key, m.Bytes)
		if err != nil {
			return true, err
		}
		c.ExternalIPConnected = m.Message
		log.Debugf("connected as %s -> %s", c.ExternalIP, c.ExternalIPConnected)
		c.Step1ChannelSecured = true
	case "error":
		// c.spinner.Stop()
		fmt.Print("\r")
		err = fmt.Errorf("peer error: %s", m.Message)
		return true, err
	case "fileinfo":
		var senderInfo SenderInfo
		err = json.Unmarshal(m.Bytes, &senderInfo)
		if err != nil {
			log.Error(err)
			return
		}
		c.FilesToTransfer = senderInfo.FilesToTransfer
		fname := fmt.Sprintf("%d files", len(c.FilesToTransfer))
		if len(c.FilesToTransfer) == 1 {
			fname = fmt.Sprintf("'%s'", c.FilesToTransfer[0].Name)
		}
		totalSize := int64(0)
		for _, fi := range c.FilesToTransfer {
			totalSize += fi.Size
		}
		// c.spinner.Stop()
		if !c.Options.NoPrompt {
			fmt.Fprintf(os.Stderr, "\rAccept %s (%s)? (y/n) ", fname, utils.ByteCountDecimal(totalSize))
			if strings.ToLower(strings.TrimSpace(utils.GetInput(""))) != "y" {
				err = message.Send(c.conn[0], c.Key, message.Message{
					Type:    "error",
					Message: "refusing files",
				})
				return true, fmt.Errorf("refused files")
			}
		} else {
			fmt.Fprintf(os.Stderr, "\rReceiving %s (%s) \n", fname, utils.ByteCountDecimal(totalSize))
		}
		fmt.Fprintf(os.Stderr, "\nReceiving (<-%s)\n", c.ExternalIPConnected)

		log.Debug(c.FilesToTransfer)
		c.Step2FileInfoTransfered = true
	case "recipientready":
		var remoteFile RemoteFileRequest
		err = json.Unmarshal(m.Bytes, &remoteFile)
		if err != nil {
			return
		}
		c.FilesToTransferCurrentNum = remoteFile.FilesToTransferCurrentNum
		c.CurrentFileChunks = remoteFile.CurrentFileChunks
		log.Debugf("current file chunks: %+v", c.CurrentFileChunks)
		c.chunkMap = make(map[uint64]struct{})
		for _, chunk := range c.CurrentFileChunks {
			c.chunkMap[uint64(chunk)] = struct{}{}
		}
		c.Step3RecipientRequestFile = true
	case "close-sender":
		c.bar.Finish()
		log.Debug("close-sender received...")
		c.Step4FileTransfer = false
		c.Step3RecipientRequestFile = false
		log.Debug("sending close-recipient")
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type: "close-recipient",
		})
	case "close-recipient":
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
	if c.Options.IsSender && c.Step1ChannelSecured && !c.Step2FileInfoTransfered {
		var b []byte
		b, err = json.Marshal(SenderInfo{
			MachineID:       c.machineID,
			FilesToTransfer: c.FilesToTransfer,
		})
		if err != nil {
			log.Error(err)
			return
		}
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:  "fileinfo",
			Bytes: b,
		})
		if err != nil {
			return
		}
		c.Step2FileInfoTransfered = true
	}
	if !c.Options.IsSender && c.Step2FileInfoTransfered && !c.Step3RecipientRequestFile {
		// find the next file to transfer and send that number
		// if the files are the same size, then look for missing chunks
		finished := true
		for i, fileInfo := range c.FilesToTransfer {
			if i < c.FilesToTransferCurrentNum {
				continue
			}
			fileHash, errHash := utils.HashFile(path.Join(fileInfo.FolderRemote, fileInfo.Name))
			if errHash != nil || !bytes.Equal(fileHash, fileInfo.Hash) {
				if !bytes.Equal(fileHash, fileInfo.Hash) {
					log.Debugf("hashes are not equal %x != %x", fileHash, fileInfo.Hash)
				}
				finished = false
				c.FilesToTransferCurrentNum = i
				break
			}
			// TODO: print out something about this file already existing
		}
		if finished {
			// TODO: do the last finishing stuff
			log.Debug("finished")
			err = message.Send(c.conn[0], c.Key, message.Message{
				Type: "finished",
			})
			if err != nil {
				panic(err)
			}
		}

		// start initiating the process to receive a new file
		log.Debugf("working on file %d", c.FilesToTransferCurrentNum)

		// recipient sets the file
		pathToFile := path.Join(
			c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote,
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
		)
		folderForFile, _ := filepath.Split(pathToFile)
		os.MkdirAll(folderForFile, os.ModePerm)
		var errOpen error
		c.CurrentFile, errOpen = os.OpenFile(
			pathToFile,
			os.O_WRONLY, 0666)
		truncate := false
		c.CurrentFileChunks = []int64{}
		if errOpen == nil {
			stat, _ := c.CurrentFile.Stat()
			truncate = stat.Size() != c.FilesToTransfer[c.FilesToTransferCurrentNum].Size
			if truncate == false {
				// recipient requests the file and chunks (if empty, then should receive all chunks)
				// TODO: determine the missing chunks
				c.CurrentFileChunks = utils.MissingChunks(
					pathToFile,
					c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
					models.TCP_BUFFER_SIZE/2,
				)
			}
		} else {
			c.CurrentFile, errOpen = os.Create(pathToFile)
			if errOpen != nil {
				errOpen = errors.Wrap(errOpen, "could not create "+pathToFile)
				log.Error(errOpen)
				return errOpen
			}
			truncate = true
		}
		if truncate {
			err := c.CurrentFile.Truncate(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size)
			if err != nil {
				err = errors.Wrap(err, "could not truncate "+pathToFile)
				log.Error(err)
				return err
			}
		}

		// setup the progressbar
		c.setBar()
		c.TotalSent = 0
		bRequest, _ := json.Marshal(RemoteFileRequest{
			CurrentFileChunks:         c.CurrentFileChunks,
			FilesToTransferCurrentNum: c.FilesToTransferCurrentNum,
		})
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:  "recipientready",
			Bytes: bRequest,
		})
		if err != nil {
			return
		}
		c.Step3RecipientRequestFile = true
	}
	if c.Options.IsSender && c.Step3RecipientRequestFile && !c.Step4FileTransfer {
		log.Debug("start sending data!")
		if !c.firstSend {
			fmt.Fprintf(os.Stderr, "\nSending (->%s)\n", c.ExternalIPConnected)
			c.firstSend = true
		}
		c.Step4FileTransfer = true
		// setup the progressbar
		c.setBar()
		c.TotalSent = 0
		for i := 0; i < len(c.Options.RelayPorts); i++ {
			go c.sendData(i)
		}
	}
	return
}

func (c *Client) setBar() {
	description := fmt.Sprintf("%-28s", c.FilesToTransfer[c.FilesToTransferCurrentNum].Name)
	if len(c.FilesToTransfer) == 1 {
		description = c.FilesToTransfer[c.FilesToTransferCurrentNum].Name
	}
	c.bar = progressbar.NewOptions64(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		progressbar.OptionOnCompletion(func() {
			fmt.Fprintf(os.Stderr, " ✔️\n")
		}),
		progressbar.OptionSetWidth(16),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetBytes64(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionThrottle(100*time.Millisecond),
	)
	c.bar.Add(len(c.CurrentFileChunks) * models.TCP_BUFFER_SIZE / 2)
}

func (c *Client) receiveData(i int) {
	for {
		log.Debug("waiting for data")
		data, err := c.conn[i+1].Receive()
		if err != nil {
			break
		}

		data, err = c.Key.Decrypt(data)
		if err != nil {
			panic(err)
		}
		data = compress.Decompress(data)

		// get position
		var position uint64
		rbuf := bytes.NewReader(data[:8])
		err = binary.Read(rbuf, binary.LittleEndian, &position)
		if err != nil {
			panic(err)
		}
		positionInt64 := int64(position)

		c.mutex.Lock()
		_, err = c.CurrentFile.WriteAt(data[8:], positionInt64)
		c.mutex.Unlock()
		if err != nil {
			panic(err)
		}
		c.bar.Add(len(data[8:]))
		c.TotalSent += int64(len(data[8:]))
		c.TotalChunksTransfered++
		log.Debugf("block: %+v", positionInt64)
		if c.TotalChunksTransfered == len(c.CurrentFileChunks) || c.TotalSent == c.FilesToTransfer[c.FilesToTransferCurrentNum].Size {
			log.Debug("finished receiving!")
			c.CurrentFile.Close()
			if c.Options.Stdout {
				pathToFile := path.Join(
					c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote,
					c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
				)
				b, _ := ioutil.ReadFile(pathToFile)
				fmt.Print(string(b))
			}
			log.Debug("sending close-sender")
			err = message.Send(c.conn[0], c.Key, message.Message{
				Type: "close-sender",
			})
			if err != nil {
				panic(err)
			}
		}
	}

	return
}

func (c *Client) sendData(i int) {
	defer func() {
		log.Debugf("finished with %d", i)
	}()
	pathToFile := path.Join(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderSource,
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
	)
	log.Debugf("opening %s to read", pathToFile)
	f, err := os.Open(pathToFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	pos := uint64(0)
	curi := float64(0)
	for {
		// Read file
		data := make([]byte, models.TCP_BUFFER_SIZE/2)
		n, err := f.Read(data)
		if err != nil {
			if err == io.EOF {
				break
			}
			panic(err)
		}

		if math.Mod(curi, float64(len(c.Options.RelayPorts))) == float64(i) {
			// check to see if this is a chunk that the recipient wants
			usableChunk := true
			c.mutex.Lock()
			if len(c.chunkMap) != 0 {
				if _, ok := c.chunkMap[pos]; !ok {
					usableChunk = false
				} else {
					delete(c.chunkMap, pos)
				}
			}
			c.mutex.Unlock()
			if usableChunk {
				// log.Debugf("sending chunk %d", pos)
				posByte := make([]byte, 8)
				binary.LittleEndian.PutUint64(posByte, pos)

				dataToSend, err := c.Key.Encrypt(
					compress.Compress(
						append(posByte, data[:n]...),
					),
				)
				if err != nil {
					panic(err)
				}

				err = c.conn[i+1].Send(dataToSend)
				if err != nil {
					panic(err)
				}
				c.bar.Add(n)
				c.TotalSent += int64(n)
				// time.Sleep(100 * time.Millisecond)
			} else {
				// log.Debugf("skipping chunk %d", pos)
			}
		}

		curi++
		pos += uint64(n)
	}

	time.Sleep(10 * time.Second)
	return
}
