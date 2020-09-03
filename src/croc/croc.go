package croc

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/denisbrodbeck/machineid"
	log "github.com/schollz/logger"
	"github.com/schollz/pake/v2"
	"github.com/schollz/peerdiscovery"
	"github.com/schollz/progressbar/v3"
	"github.com/schollz/spinner"
	"github.com/tscholl2/siec"

	"github.com/schollz/croc/v8/src/comm"
	"github.com/schollz/croc/v8/src/compress"
	"github.com/schollz/croc/v8/src/crypt"
	"github.com/schollz/croc/v8/src/message"
	"github.com/schollz/croc/v8/src/models"
	"github.com/schollz/croc/v8/src/tcp"
	"github.com/schollz/croc/v8/src/utils"
)

func init() {
	log.SetLevel("debug")
}

// Debug toggles debug mode
func Debug(debug bool) {
	if debug {
		log.SetLevel("debug")
	} else {
		log.SetLevel("warn")
	}
}

// Options specifies user specific options
type Options struct {
	IsSender       bool
	SharedSecret   string
	Debug          bool
	RelayAddress   string
	RelayAddress6  string
	RelayPorts     []string
	RelayPassword  string
	Stdout         bool
	NoPrompt       bool
	NoMultiplexing bool
	DisableLocal   bool
	Ask            bool
	SendingText    bool
}

// Client holds the state of the croc transfer
type Client struct {
	Options                         Options
	Pake                            *pake.Pake
	Key                             []byte
	ExternalIP, ExternalIPConnected string

	// steps involved in forming relationship
	Step1ChannelSecured       bool
	Step2FileInfoTransfered   bool
	Step3RecipientRequestFile bool
	Step4FileTransfer         bool
	Step5CloseChannels        bool
	SuccessfulTransfer        bool

	// send / receive information of all files
	FilesToTransfer           []FileInfo
	FilesToTransferCurrentNum int
	FilesHasFinished          map[int]struct{}

	// send / receive information of current file
	CurrentFile            *os.File
	CurrentFileChunkRanges []int64
	CurrentFileChunks      []int64

	TotalSent             int64
	TotalChunksTransfered int
	chunkMap              map[uint64]struct{}

	// tcp connections
	conn []*comm.Comm

	bar             *progressbar.ProgressBar
	spinner         *spinner.Spinner
	longestFilename int
	firstSend       bool

	mutex       *sync.Mutex
	fread       *os.File
	numfinished int
	quit        chan bool
}

// Chunk contains information about the
// needed bytes
type Chunk struct {
	Bytes    []byte `json:"b,omitempty"`
	Location int64  `json:"l,omitempty"`
}

// FileInfo registers the information about the file
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

// RemoteFileRequest requests specific bytes
type RemoteFileRequest struct {
	CurrentFileChunkRanges    []int64
	FilesToTransferCurrentNum int
	MachineID                 string
}

// SenderInfo lists the files to be transferred
type SenderInfo struct {
	FilesToTransfer []FileInfo
	MachineID       string
	Ask             bool
	SendingText     bool
}

// New establishes a new connection for transferring files between two instances.
func New(ops Options) (c *Client, err error) {
	c = new(Client)
	c.FilesHasFinished = make(map[int]struct{})

	// setup basic info
	c.Options = ops
	Debug(c.Options.Debug)
	log.Debugf("options: %+v", c.Options)

	if len(c.Options.SharedSecret) < 4 {
		err = fmt.Errorf("code is too short")
		return
	}

	c.conn = make([]*comm.Comm, 16)

	// initialize pake
	if c.Options.IsSender {
		c.Pake, err = pake.Init([]byte(c.Options.SharedSecret), 1, siec.SIEC255(), 1*time.Microsecond)
	} else {
		c.Pake, err = pake.Init([]byte(c.Options.SharedSecret), 0, siec.SIEC255(), 1*time.Microsecond)
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

func (c *Client) sendCollectFiles(options TransferOptions) (err error) {
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
		if len(fstats.Name()) > c.longestFilename {
			c.longestFilename = len(fstats.Name())
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
	log.Debugf("longestFilename: %+v", c.longestFilename)
	fname := fmt.Sprintf("%d files", len(c.FilesToTransfer))
	if len(c.FilesToTransfer) == 1 {
		fname = fmt.Sprintf("'%s'", c.FilesToTransfer[0].Name)
	}
	fmt.Fprintf(os.Stderr, "Sending %s (%s)\n", fname, utils.ByteCountDecimal(totalFilesSize))
	return
}

func (c *Client) setupLocalRelay() {
	// setup the relay locally
	for _, port := range c.Options.RelayPorts {
		go func(portStr string) {
			debugString := "warn"
			if c.Options.Debug {
				debugString = "debug"
			}
			err := tcp.Run(debugString, portStr, c.Options.RelayPassword, strings.Join(c.Options.RelayPorts[1:], ","))
			if err != nil {
				panic(err)
			}
		}(port)
	}
}

func (c *Client) broadcastOnLocalNetwork(useipv6 bool) {
	// look for peers first
	settings := peerdiscovery.Settings{
		Limit:     -1,
		Payload:   []byte("croc" + c.Options.RelayPorts[0]),
		Delay:     10 * time.Millisecond,
		TimeLimit: 30 * time.Second,
	}
	if useipv6 {
		settings.IPVersion = peerdiscovery.IPv6
	}

	discoveries, err := peerdiscovery.Discover(settings)
	log.Debugf("discoveries: %+v", discoveries)

	if err != nil {
		log.Debug(err)
	}
}

func (c *Client) transferOverLocalRelay(options TransferOptions, errchan chan<- error) {
	time.Sleep(500 * time.Millisecond)
	log.Debug("establishing connection")
	var banner string
	conn, banner, ipaddr, err := tcp.ConnectToTCPServer("localhost:"+c.Options.RelayPorts[0], c.Options.RelayPassword, c.Options.SharedSecret[:3])
	log.Debugf("banner: %s", banner)
	if err != nil {
		err = fmt.Errorf("could not connect to localhost:%s: %w", c.Options.RelayPorts[0], err)
		log.Debug(err)
		// not really an error because it will try to connect over the actual relay
		return
	}
	log.Debugf("local connection established: %+v", conn)
	for {
		data, _ := conn.Receive()
		if bytes.Equal(data, []byte("handshake")) {
			break
		} else if bytes.Equal(data, []byte{1}) {
			log.Debug("got ping")
		} else {
			log.Debugf("instead of handshake got: %s", data)
		}
	}
	c.conn[0] = conn
	log.Debug("exchanged header message")
	c.Options.RelayAddress = "localhost"
	c.Options.RelayPorts = strings.Split(banner, ",")
	if c.Options.NoMultiplexing {
		log.Debug("no multiplexing")
		c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
	}
	c.ExternalIP = ipaddr
	errchan <- c.transfer(options)
}

// Send will send the specified file
func (c *Client) Send(options TransferOptions) (err error) {
	err = c.sendCollectFiles(options)
	if err != nil {
		return
	}
	otherRelay := ""
	if c.Options.RelayAddress != models.DEFAULT_RELAY {
		otherRelay = "--relay " + c.Options.RelayAddress + " "
	}
	fmt.Fprintf(os.Stderr, "Code is: %s\nOn the other computer run\n\ncroc %s%s\n", c.Options.SharedSecret, otherRelay, c.Options.SharedSecret)
	if c.Options.Ask {
		machid, _ := machineid.ID()
		fmt.Fprintf(os.Stderr, "\rYour machine ID is '%s'\n", machid)
	}
	// // c.spinner.Suffix = " waiting for recipient..."
	// c.spinner.Start()
	// create channel for quitting
	// connect to the relay for messaging
	errchan := make(chan error, 1)

	if !c.Options.DisableLocal {
		// add two things to the error channel
		errchan = make(chan error, 2)
		c.setupLocalRelay()
		go c.broadcastOnLocalNetwork(true)
		go c.broadcastOnLocalNetwork(false)
		go c.transferOverLocalRelay(options, errchan)
	}

	go func() {
		var ipaddr, banner string
		var conn *comm.Comm
		durations := []time.Duration{100 * time.Millisecond, 5 * time.Second}
		for i, address := range []string{c.Options.RelayAddress6, c.Options.RelayAddress} {
			if address == "" {
				continue
			}
			host, port, _ := net.SplitHostPort(address)
			log.Debugf("host: '%s', port: '%s'", host, port)
			// Default port to :9009
			if port == "" {
				host = address
				port = "9009"
			}
			log.Debugf("got host '%v' and port '%v'", host, port)
			address = net.JoinHostPort(host, port)
			log.Debugf("trying connection to %s", address)
			conn, banner, ipaddr, err = tcp.ConnectToTCPServer(address, c.Options.RelayPassword, c.Options.SharedSecret[:3], durations[i])
			if err == nil {
				c.Options.RelayAddress = address
				break
			}
			log.Debugf("could not establish '%s'", address)
		}
		if conn == nil && err == nil {
			err = fmt.Errorf("could not connect")
		}
		if err != nil {
			err = fmt.Errorf("could not connect to %s: %w", c.Options.RelayAddress, err)
			log.Debug(err)
			errchan <- err
			return
		}
		log.Debugf("banner: %s", banner)
		log.Debugf("connection established: %+v", conn)
		for {
			log.Debug("waiting for bytes")
			data, errConn := conn.Receive()
			if errConn != nil {
				log.Debugf("[%+v] had error: %s", conn, errConn.Error())
			}
			if bytes.Equal(data, []byte("ips?")) {
				// recipient wants to try to connect to local ips
				var ips []string
				// only get local ips if the local is enabled
				if !c.Options.DisableLocal {
					// get list of local ips
					ips, err = utils.GetLocalIPs()
					if err != nil {
						log.Debugf("error getting local ips: %v", err)
					}
					// prepend the port that is being listened to
					ips = append([]string{c.Options.RelayPorts[0]}, ips...)
				}
				bips, _ := json.Marshal(ips)
				if err := conn.Send(bips); err != nil {
					log.Errorf("error sending: %v", err)
				}
			} else if bytes.Equal(data, []byte("handshake")) {
				break
			} else if bytes.Equal(data, []byte{1}) {
				log.Debug("got ping")
				continue
			} else {
				log.Debugf("[%+v] got weird bytes: %+v", conn, data)
				// throttle the reading
				errchan <- fmt.Errorf("gracefully refusing using the public relay")
				return
			}
		}

		c.conn[0] = conn
		c.Options.RelayPorts = strings.Split(banner, ",")
		if c.Options.NoMultiplexing {
			log.Debug("no multiplexing")
			c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
		}
		c.ExternalIP = ipaddr
		log.Debug("exchanged header message")
		errchan <- c.transfer(options)
	}()

	err = <-errchan
	if err == nil {
		// return if no error
		return
	} else {
		log.Debugf("error from errchan: %v", err)
	}
	if !c.Options.DisableLocal {
		if strings.Contains(err.Error(), "refusing files") || strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "bad password") {
			errchan <- err
		}
		err = <-errchan
	}
	return err
}

// Receive will receive a file
func (c *Client) Receive() (err error) {
	fmt.Fprintf(os.Stderr, "connecting...")
	// recipient will look for peers first
	// and continue if it doesn't find any within 100 ms
	usingLocal := false
	if !c.Options.DisableLocal {
		log.Debug("attempt to discover peers")
		var discoveries []peerdiscovery.Discovered
		var wgDiscovery sync.WaitGroup
		var dmux sync.Mutex
		wgDiscovery.Add(2)
		go func() {
			defer wgDiscovery.Done()
			ipv4discoveries, err1 := peerdiscovery.Discover(peerdiscovery.Settings{
				Limit:     1,
				Payload:   []byte("ok"),
				Delay:     10 * time.Millisecond,
				TimeLimit: 200 * time.Millisecond,
			})
			if err1 == nil && len(ipv4discoveries) > 0 {
				dmux.Lock()
				err = err1
				discoveries = ipv4discoveries
				dmux.Unlock()
			}
		}()
		go func() {
			defer wgDiscovery.Done()
			ipv6discoveries, err1 := peerdiscovery.Discover(peerdiscovery.Settings{
				Limit:     1,
				Payload:   []byte("ok"),
				Delay:     10 * time.Millisecond,
				TimeLimit: 200 * time.Millisecond,
				IPVersion: peerdiscovery.IPv6,
			})
			if err1 == nil && len(ipv6discoveries) > 0 {
				dmux.Lock()
				err = err1
				discoveries = ipv6discoveries
				dmux.Unlock()
			}
		}()
		wgDiscovery.Wait()

		if err == nil && len(discoveries) > 0 {
			for i := 0; i < len(discoveries); i++ {
				log.Debugf("discovery %d has payload: %+v", i, discoveries[i])
				if !bytes.HasPrefix(discoveries[i].Payload, []byte("croc")) {
					log.Debug("skipping discovery")
					continue
				}
				log.Debug("switching to local")
				portToUse := string(bytes.TrimPrefix(discoveries[0].Payload, []byte("croc")))
				if portToUse == "" {
					portToUse = "9009"
				}
				c.Options.RelayAddress = net.JoinHostPort(discoveries[0].Address, portToUse)
				c.ExternalIPConnected = c.Options.RelayAddress
				c.Options.RelayAddress6 = ""
				usingLocal = true

				break
			}
		}
		log.Debugf("discoveries: %+v", discoveries)
		log.Debug("establishing connection")
	}
	var banner string
	durations := []time.Duration{100 * time.Millisecond, 5 * time.Second}
	for i, address := range []string{c.Options.RelayAddress6, c.Options.RelayAddress} {
		if address == "" {
			continue
		}
		var host, port string
		host, port, _ = net.SplitHostPort(address)
		// Default port to :9009
		if port == "" {
			host = address
			port = "9009"
		}
		log.Debugf("got host '%v' and port '%v'", host, port)
		address = net.JoinHostPort(host, port)
		log.Debugf("trying connection to %s", address)
		c.conn[0], banner, c.ExternalIP, err = tcp.ConnectToTCPServer(address, c.Options.RelayPassword, c.Options.SharedSecret[:3], durations[i])
		if err == nil {
			c.Options.RelayAddress = address
			break
		}
		log.Debugf("could not establish '%s'", address)
	}
	if err != nil {
		err = fmt.Errorf("could not connect to %s: %w", c.Options.RelayAddress, err)
		log.Debug(err)
		return
	}
	log.Debugf("receiver connection established: %+v", c.conn[0])
	log.Debugf("banner: %s", banner)

	if !usingLocal && !c.Options.DisableLocal {
		// ask the sender for their local ips and port
		// and try to connect to them
		log.Debug("sending ips?")
		var data []byte
		if err := c.conn[0].Send([]byte("ips?")); err != nil {
			log.Errorf("ips send error: %v", err)
		}
		data, err = c.conn[0].Receive()
		if err != nil {
			return
		}
		log.Debugf("ips data: %s", data)
		var ips []string
		if err := json.Unmarshal(data, &ips); err != nil {
			log.Errorf("ips unmarshal error: %v", err)
		}
		if len(ips) > 1 {
			port := ips[0]
			ips = ips[1:]
			for _, ip := range ips {
				ipv4Addr, ipv4Net, errNet := net.ParseCIDR(fmt.Sprintf("%s/24", ip))
				log.Debugf("ipv4Add4: %+v, ipv4Net: %+v, err: %+v", ipv4Addr, ipv4Net, errNet)
				localIps, _ := utils.GetLocalIPs()
				haveLocalIP := false
				for _, localIP := range localIps {
					localIPparsed := net.ParseIP(localIP)
					if ipv4Net.Contains(localIPparsed) {
						haveLocalIP = true
						break
					}
				}
				if !haveLocalIP {
					log.Debugf("%s is not a local IP, skipping", ip)
					continue
				}

				serverTry := fmt.Sprintf("%s:%s", ip, port)
				conn, banner2, externalIP, errConn := tcp.ConnectToTCPServer(serverTry, c.Options.RelayPassword, c.Options.SharedSecret[:3], 50*time.Millisecond)
				if errConn != nil {
					log.Debugf("could not connect to " + serverTry)
					continue
				}
				log.Debugf("local connection established to %s", serverTry)
				log.Debugf("banner: %s", banner2)
				// reset to the local port
				banner = banner2
				c.Options.RelayAddress = serverTry
				c.ExternalIP = externalIP
				c.conn[0].Close()
				c.conn[0] = nil
				c.conn[0] = conn
				break
			}
		}
	}

	if err := c.conn[0].Send([]byte("handshake")); err != nil {
		log.Errorf("handshake send error: %v", err)
	}
	c.Options.RelayPorts = strings.Split(banner, ",")
	if c.Options.NoMultiplexing {
		log.Debug("no multiplexing")
		c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
	}
	log.Debug("exchanged header message")
	fmt.Fprintf(os.Stderr, "\rsecuring channel...")
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
			log.Debugf("got error receiving: %v", err)
			if !c.Step1ChannelSecured {
				err = fmt.Errorf("could not secure channel")
			}
			break
		}
		done, err = c.processMessage(data)
		if err != nil {
			log.Debugf("got error processing: %v", err)
			break
		}
		if done {
			break
		}
	}
	// purge errors that come from successful transfer
	if c.SuccessfulTransfer {
		if err != nil {
			log.Debugf("purging error: %s", err)
		}
		err = nil
	}

	if c.Options.Stdout && !c.Options.IsSender {
		pathToFile := path.Join(
			c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote,
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
		)
		log.Debugf("pathToFile: %s", pathToFile)
		// close if not closed already
		c.CurrentFile.Close()
		if err := os.Remove(pathToFile); err != nil {
			log.Warnf("error removing %s: %v", pathToFile, err)
		}
		fmt.Print("\n")
	}
	return
}

func (c *Client) processMessageFileInfo(m message.Message) (done bool, err error) {
	var senderInfo SenderInfo
	err = json.Unmarshal(m.Bytes, &senderInfo)
	if err != nil {
		log.Error(err)
		return
	}
	c.Options.SendingText = senderInfo.SendingText
	if c.Options.SendingText {
		c.Options.Stdout = true
	}
	c.FilesToTransfer = senderInfo.FilesToTransfer
	fname := fmt.Sprintf("%d files", len(c.FilesToTransfer))
	if len(c.FilesToTransfer) == 1 {
		fname = fmt.Sprintf("'%s'", c.FilesToTransfer[0].Name)
	}
	totalSize := int64(0)
	for _, fi := range c.FilesToTransfer {
		totalSize += fi.Size
		if len(fi.Name) > c.longestFilename {
			c.longestFilename = len(fi.Name)
		}
	}
	// c.spinner.Stop()
	if !c.Options.NoPrompt || c.Options.Ask || senderInfo.Ask {
		if c.Options.Ask || senderInfo.Ask {
			machID, _ := machineid.ID()
			fmt.Fprintf(os.Stderr, "\rYour machine id is '%s'.\nAccept %s (%s) from '%s'? (y/n) ", machID, fname, utils.ByteCountDecimal(totalSize), senderInfo.MachineID)
		} else {
			fmt.Fprintf(os.Stderr, "\rAccept %s (%s)? (y/n) ", fname, utils.ByteCountDecimal(totalSize))
		}
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
	return
}

func (c *Client) procesMessagePake(m message.Message) (err error) {
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
			if _, rerr := rand.Read(salt); err != nil {
				log.Errorf("can't generate random numbers: %v", rerr)
				return
			}
			err = message.Send(c.conn[0], c.Key, message.Message{
				Type:  "salt",
				Bytes: salt,
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
				var host string
				if c.Options.RelayAddress == "localhost" {
					host = c.Options.RelayAddress
				} else {
					host, _, err = net.SplitHostPort(c.Options.RelayAddress)
					if err != nil {
						log.Errorf("bad relay address %s", c.Options.RelayAddress)
						return
					}
				}
				server := net.JoinHostPort(host, c.Options.RelayPorts[j])
				log.Debugf("connecting to %s", server)
				c.conn[j+1], _, _, err = tcp.ConnectToTCPServer(
					server,
					c.Options.RelayPassword,
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
	return
}

func (c *Client) processMessageSalt(m message.Message) (done bool, err error) {
	log.Debug("received salt")
	if !c.Options.IsSender {
		log.Debug("sending salt back")
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:  "salt",
			Bytes: m.Bytes,
		})
	}
	log.Debugf("session key is verified, generating encryption with salt: %x", m.Bytes)
	key, err := c.Pake.SessionKey()
	if err != nil {
		return true, err
	}
	c.Key, _, err = crypt.New(key, m.Bytes)
	if err != nil {
		return true, err
	}
	log.Debugf("key = %+x", c.Key)
	if c.Options.IsSender {
		log.Debug("sending external IP")
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:    "externalip",
			Message: c.ExternalIP,
			Bytes:   m.Bytes,
		})
	}
	return
}

func (c *Client) processExternalIP(m message.Message) (done bool, err error) {
	log.Debugf("received external IP: %+v", m)
	if !c.Options.IsSender {
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:    "externalip",
			Message: c.ExternalIP,
		})
		if err != nil {
			return true, err
		}
	}
	if c.ExternalIPConnected == "" {
		// it can be preset by the local relay
		c.ExternalIPConnected = m.Message
	}
	log.Debugf("connected as %s -> %s", c.ExternalIP, c.ExternalIPConnected)
	c.Step1ChannelSecured = true
	return
}

func (c *Client) processMessage(payload []byte) (done bool, err error) {
	m, err := message.Decode(c.Key, payload)
	if err != nil {
		err = fmt.Errorf("problem with decoding: %w", err)
		log.Debug(err)
		return
	}

	switch m.Type {
	case "finished":
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type: "finished",
		})
		done = true
		c.SuccessfulTransfer = true
		return
	case "pake":
		err = c.procesMessagePake(m)
		if err != nil {
			err = fmt.Errorf("pake not successful: %w", err)
			log.Debug(err)
		}
	case "salt":
		done, err = c.processMessageSalt(m)
	case "externalip":
		done, err = c.processExternalIP(m)
	case "error":
		// c.spinner.Stop()
		fmt.Print("\r")
		err = fmt.Errorf("peer error: %s", m.Message)
		return true, err
	case "fileinfo":
		done, err = c.processMessageFileInfo(m)
	case "recipientready":
		var remoteFile RemoteFileRequest
		err = json.Unmarshal(m.Bytes, &remoteFile)
		if err != nil {
			return
		}
		c.FilesToTransferCurrentNum = remoteFile.FilesToTransferCurrentNum
		c.CurrentFileChunkRanges = remoteFile.CurrentFileChunkRanges
		c.CurrentFileChunks = utils.ChunkRangesToChunks(c.CurrentFileChunkRanges)
		log.Debugf("current file chunks: %+v", c.CurrentFileChunks)
		c.mutex.Lock()
		c.chunkMap = make(map[uint64]struct{})
		for _, chunk := range c.CurrentFileChunks {
			c.chunkMap[uint64(chunk)] = struct{}{}
		}
		c.mutex.Unlock()
		c.Step3RecipientRequestFile = true

		if c.Options.Ask {
			fmt.Fprintf(os.Stderr, "Send to machine '%s'? (y/n) ", remoteFile.MachineID)
			if strings.ToLower(strings.TrimSpace(utils.GetInput(""))) != "y" {
				err = message.Send(c.conn[0], c.Key, message.Message{
					Type:    "error",
					Message: "refusing files",
				})
				done = true
				err = fmt.Errorf("refused files")
				return
			}
		}
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
		log.Debugf("got error from processing message: %v", err)
		return
	}
	err = c.updateState()
	if err != nil {
		log.Debugf("got error from updating state: %v", err)
		return
	}
	return
}

func (c *Client) updateIfSenderChannelSecured() (err error) {
	if c.Options.IsSender && c.Step1ChannelSecured && !c.Step2FileInfoTransfered {
		var b []byte
		machID, _ := machineid.ID()
		b, err = json.Marshal(SenderInfo{
			FilesToTransfer: c.FilesToTransfer,
			MachineID:       machID,
			Ask:             c.Options.Ask,
			SendingText:     c.Options.SendingText,
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
	return
}

func (c *Client) recipientInitializeFile() (err error) {
	// start initiating the process to receive a new file
	log.Debugf("working on file %d", c.FilesToTransferCurrentNum)

	// recipient sets the file
	pathToFile := path.Join(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote,
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
	)
	folderForFile, _ := filepath.Split(pathToFile)
	folderForFileBase := filepath.Base(folderForFile)
	if folderForFileBase != "." && folderForFileBase != "" {
		if err := os.MkdirAll(folderForFile, os.ModePerm); err != nil {
			log.Errorf("can't create %s: %v", folderForFile, err)
		}
	}
	var errOpen error
	c.CurrentFile, errOpen = os.OpenFile(
		pathToFile,
		os.O_WRONLY, 0666)
	var truncate bool // default false
	c.CurrentFileChunks = []int64{}
	c.CurrentFileChunkRanges = []int64{}
	if errOpen == nil {
		stat, _ := c.CurrentFile.Stat()
		truncate = stat.Size() != c.FilesToTransfer[c.FilesToTransferCurrentNum].Size
		if truncate == false {
			// recipient requests the file and chunks (if empty, then should receive all chunks)
			// TODO: determine the missing chunks
			c.CurrentFileChunkRanges = utils.MissingChunks(
				pathToFile,
				c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
				models.TCP_BUFFER_SIZE/2,
			)
		}
	} else {
		c.CurrentFile, errOpen = os.Create(pathToFile)
		if errOpen != nil {
			errOpen = fmt.Errorf("could not create %s: %w", pathToFile, errOpen)
			log.Error(errOpen)
			return errOpen
		}
		truncate = true
	}
	if truncate {
		err := c.CurrentFile.Truncate(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size)
		if err != nil {
			err = fmt.Errorf("could not truncate %s: %w", pathToFile, err)
			log.Error(err)
			return err
		}
	}
	return
}

func (c *Client) recipientGetFileReady(finished bool) (err error) {
	if finished {
		// TODO: do the last finishing stuff
		log.Debug("finished")
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type: "finished",
		})
		if err != nil {
			panic(err)
		}
		c.SuccessfulTransfer = true
		c.FilesHasFinished[c.FilesToTransferCurrentNum] = struct{}{}
	}

	err = c.recipientInitializeFile()
	if err != nil {
		return
	}

	c.TotalSent = 0
	machID, _ := machineid.ID()
	bRequest, _ := json.Marshal(RemoteFileRequest{
		CurrentFileChunkRanges:    c.CurrentFileChunkRanges,
		FilesToTransferCurrentNum: c.FilesToTransferCurrentNum,
		MachineID:                 machID,
	})
	log.Debug("converting to chunk range")
	c.CurrentFileChunks = utils.ChunkRangesToChunks(c.CurrentFileChunkRanges)

	if !finished {
		// setup the progressbar
		c.setBar()
	}

	log.Debugf("sending recipient ready with %d chunks", len(c.CurrentFileChunks))
	err = message.Send(c.conn[0], c.Key, message.Message{
		Type:  "recipientready",
		Bytes: bRequest,
	})
	if err != nil {
		return
	}
	c.Step3RecipientRequestFile = true
	return
}

func (c *Client) createEmptyFileAndFinish(fileInfo FileInfo, i int) (err error) {
	log.Debugf("touching file with folder / name")
	if !utils.Exists(fileInfo.FolderRemote) {
		err = os.MkdirAll(fileInfo.FolderRemote, os.ModePerm)
		if err != nil {
			log.Error(err)
			return
		}
	}
	emptyFile, errCreate := os.Create(path.Join(fileInfo.FolderRemote, fileInfo.Name))
	if errCreate != nil {
		log.Error(errCreate)
		err = errCreate
		return
	}
	emptyFile.Close()
	// setup the progressbar
	description := fmt.Sprintf("%-*s", c.longestFilename, c.FilesToTransfer[i].Name)
	if len(c.FilesToTransfer) == 1 {
		description = c.FilesToTransfer[i].Name
	}
	c.bar = progressbar.NewOptions64(1,
		progressbar.OptionOnCompletion(func() {
			fmt.Fprintf(os.Stderr, " ✔️\n")
		}),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetVisibility(!c.Options.SendingText),
	)
	c.bar.Finish()
	return
}

func (c *Client) updateIfRecipientHasFileInfo() (err error) {
	if !(!c.Options.IsSender && c.Step2FileInfoTransfered && !c.Step3RecipientRequestFile) {
		return
	}
	// find the next file to transfer and send that number
	// if the files are the same size, then look for missing chunks
	finished := true

	for i, fileInfo := range c.FilesToTransfer {
		if _, ok := c.FilesHasFinished[i]; ok {
			continue
		}
		log.Debugf("checking %+v", fileInfo)
		if i < c.FilesToTransferCurrentNum {
			continue
		}
		fileHash, errHash := utils.HashFile(path.Join(fileInfo.FolderRemote, fileInfo.Name))
		if fileInfo.Size == 0 {
			err = c.createEmptyFileAndFinish(fileInfo, i)
			if err != nil {
				return
			}
			continue
		}
		log.Debugf("%s %+x %+x %+v", fileInfo.Name, fileHash, fileInfo.Hash, errHash)
		if !bytes.Equal(fileHash, fileInfo.Hash) {
			log.Debugf("hashes are not equal %x != %x", fileHash, fileInfo.Hash)
		} else {
			log.Debugf("hashes are equal %x == %x", fileHash, fileInfo.Hash)
		}
		if errHash != nil {
			// probably can't find, its okay
			log.Debug(errHash)
		}
		if errHash != nil || !bytes.Equal(fileHash, fileInfo.Hash) {
			finished = false
			c.FilesToTransferCurrentNum = i
			break
		}
		// TODO: print out something about this file already existing
	}
	err = c.recipientGetFileReady(finished)
	return
}

func (c *Client) updateState() (err error) {
	err = c.updateIfSenderChannelSecured()
	if err != nil {
		return
	}

	err = c.updateIfRecipientHasFileInfo()
	if err != nil {
		return
	}

	if c.Options.IsSender && c.Step3RecipientRequestFile && !c.Step4FileTransfer {
		log.Debug("start sending data!")

		if !c.firstSend {
			fmt.Fprintf(os.Stderr, "\nSending (->%s)\n", c.ExternalIPConnected)
			c.firstSend = true
			// if there are empty files, show them as already have been transferred now
			for i := range c.FilesToTransfer {
				if c.FilesToTransfer[i].Size == 0 {
					// setup the progressbar and takedown the progress bar for empty files
					description := fmt.Sprintf("%-*s", c.longestFilename, c.FilesToTransfer[i].Name)
					if len(c.FilesToTransfer) == 1 {
						description = c.FilesToTransfer[i].Name
					}
					c.bar = progressbar.NewOptions64(1,
						progressbar.OptionOnCompletion(func() {
							fmt.Fprintf(os.Stderr, " ✔️\n")
						}),
						progressbar.OptionSetWidth(20),
						progressbar.OptionSetDescription(description),
						progressbar.OptionSetRenderBlankState(true),
						progressbar.OptionShowBytes(true),
						progressbar.OptionShowCount(),
						progressbar.OptionSetWriter(os.Stderr),
						progressbar.OptionSetVisibility(!c.Options.SendingText),
					)
					c.bar.Finish()
				}
			}
		}
		c.Step4FileTransfer = true
		// setup the progressbar
		c.setBar()
		c.TotalSent = 0
		log.Debug("beginning sending comms")
		pathToFile := path.Join(
			c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderSource,
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
		)

		c.fread, err = os.Open(pathToFile)
		c.numfinished = 0
		if err != nil {
			return
		}
		for i := 0; i < len(c.Options.RelayPorts); i++ {
			log.Debugf("starting sending over comm %d", i)
			go c.sendData(i)
		}
	}
	return
}

func (c *Client) setBar() {
	description := fmt.Sprintf("%-*s", c.longestFilename, c.FilesToTransfer[c.FilesToTransferCurrentNum].Name)
	if len(c.FilesToTransfer) == 1 {
		description = c.FilesToTransfer[c.FilesToTransferCurrentNum].Name
	}
	c.bar = progressbar.NewOptions64(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		progressbar.OptionOnCompletion(func() {
			fmt.Fprintf(os.Stderr, " ✔️\n")
		}),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionSetVisibility(!c.Options.SendingText),
	)
	byteToDo := int64(len(c.CurrentFileChunks) * models.TCP_BUFFER_SIZE / 2)
	if byteToDo > 0 {
		bytesDone := c.FilesToTransfer[c.FilesToTransferCurrentNum].Size - byteToDo
		log.Debug(byteToDo)
		log.Debug(c.FilesToTransfer[c.FilesToTransferCurrentNum].Size)
		log.Debug(bytesDone)
		if bytesDone > 0 {
			c.bar.Add64(bytesDone)
		}
	}
}

func (c *Client) receiveData(i int) {
	log.Debugf("%d receiving data", i)
	for {
		data, err := c.conn[i+1].Receive()
		if err != nil {
			break
		}
		if bytes.Equal(data, []byte{1}) {
			log.Debug("got ping")
			continue
		}

		data, err = crypt.Decrypt(data, c.Key)
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
		if c.TotalChunksTransfered == len(c.CurrentFileChunks) || c.TotalSent == c.FilesToTransfer[c.FilesToTransferCurrentNum].Size {
			log.Debug("finished receiving!")
			if err := c.CurrentFile.Close(); err != nil {
				log.Errorf("error closing %s: %v", c.CurrentFile.Name(), err)
			}
			if c.Options.Stdout || strings.HasPrefix(c.FilesToTransfer[c.FilesToTransferCurrentNum].Name, "croc-stdin") {
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
		c.numfinished++
		if c.numfinished == len(c.Options.RelayPorts) {
			log.Debug("closing file")
			if err := c.fread.Close(); err != nil {
				log.Errorf("error closing file: %v", err)
			}
		}
	}()

	var readingPos int64
	pos := uint64(0)
	curi := float64(0)
	for {
		// Read file
		data := make([]byte, models.TCP_BUFFER_SIZE/2)
		// log.Debugf("%d trying to read", i)
		n, errRead := c.fread.ReadAt(data, readingPos)
		// log.Debugf("%d read %d bytes", i, n)
		readingPos += int64(n)

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

				dataToSend, err := crypt.Encrypt(
					compress.Compress(
						append(posByte, data[:n]...),
					),
					c.Key,
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

		if errRead != nil {
			if errRead == io.EOF {
				break
			}
			panic(errRead)
		}
	}
	return
}
