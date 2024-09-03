package croc

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
	"golang.org/x/time/rate"

	"github.com/denisbrodbeck/machineid"
	ignore "github.com/sabhiram/go-gitignore"
	log "github.com/schollz/logger"
	"github.com/schollz/pake/v3"
	"github.com/schollz/peerdiscovery"
	"github.com/schollz/progressbar/v3"

	"github.com/schollz/croc/v10/src/comm"
	"github.com/schollz/croc/v10/src/compress"
	"github.com/schollz/croc/v10/src/crypt"
	"github.com/schollz/croc/v10/src/message"
	"github.com/schollz/croc/v10/src/models"
	"github.com/schollz/croc/v10/src/tcp"
	"github.com/schollz/croc/v10/src/utils"
)

var (
	ipRequest        = []byte("ips?")
	handshakeRequest = []byte("handshake")
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
	RoomName       string
	Debug          bool
	RelayAddress   string
	RelayAddress6  string
	RelayPorts     []string
	RelayPassword  string
	Stdout         bool
	NoPrompt       bool
	NoMultiplexing bool
	DisableLocal   bool
	OnlyLocal      bool
	IgnoreStdin    bool
	Ask            bool
	SendingText    bool
	NoCompress     bool
	IP             string
	Overwrite      bool
	Curve          string
	HashAlgorithm  string
	ThrottleUpload string
	ZipFolder      bool
	TestFlag       bool
	GitIgnore      bool
}

type SimpleMessage struct {
	Bytes []byte
	Kind  string
}

// Client holds the state of the croc transfer
type Client struct {
	Options                         Options
	Pake                            *pake.Pake
	Key                             []byte
	ExternalIP, ExternalIPConnected string

	// steps involved in forming relationship
	Step1ChannelSecured       bool
	Step2FileInfoTransferred  bool
	Step3RecipientRequestFile bool
	Step4FileTransferred      bool
	Step5CloseChannels        bool
	SuccessfulTransfer        bool

	// send / receive information of all files
	FilesToTransfer           []FileInfo
	EmptyFoldersToTransfer    []FileInfo
	TotalNumberOfContents     int
	TotalNumberFolders        int
	FilesToTransferCurrentNum int
	FilesHasFinished          map[int]struct{}
	TotalFilesIgnored         int

	// send / receive information of current file
	CurrentFile            *os.File
	CurrentFileChunkRanges []int64
	CurrentFileChunks      []int64
	CurrentFileIsClosed    bool
	LastFolder             string

	TotalSent              int64
	TotalChunksTransferred int
	chunkMap               map[uint64]struct{}
	limiter                *rate.Limiter

	// tcp connections
	conn []*comm.Comm

	bar             *progressbar.ProgressBar
	longestFilename int
	firstSend       bool

	mutex                    *sync.Mutex
	fread                    *os.File
	numfinished              int
	quit                     chan bool
	finishedNum              int
	numberOfTransferredFiles int
}

// Chunk contains information about the
// needed bytes
type Chunk struct {
	Bytes    []byte `json:"b,omitempty"`
	Location int64  `json:"l,omitempty"`
}

// FileInfo registers the information about the file
type FileInfo struct {
	Name         string      `json:"n,omitempty"`
	FolderRemote string      `json:"fr,omitempty"`
	FolderSource string      `json:"fs,omitempty"`
	Hash         []byte      `json:"h,omitempty"`
	Size         int64       `json:"s,omitempty"`
	ModTime      time.Time   `json:"m,omitempty"`
	IsCompressed bool        `json:"c,omitempty"`
	IsEncrypted  bool        `json:"e,omitempty"`
	Symlink      string      `json:"sy,omitempty"`
	Mode         os.FileMode `json:"md,omitempty"`
	TempFile     bool        `json:"tf,omitempty"`
	IsIgnored    bool        `json:"ig,omitempty"`
}

// RemoteFileRequest requests specific bytes
type RemoteFileRequest struct {
	CurrentFileChunkRanges    []int64
	FilesToTransferCurrentNum int
	MachineID                 string
}

// SenderInfo lists the files to be transferred
type SenderInfo struct {
	FilesToTransfer        []FileInfo
	EmptyFoldersToTransfer []FileInfo
	TotalNumberFolders     int
	MachineID              string
	Ask                    bool
	SendingText            bool
	NoCompress             bool
	HashAlgorithm          string
}

// New establishes a new connection for transferring files between two instances.
func New(ops Options) (c *Client, err error) {
	c = new(Client)
	c.FilesHasFinished = make(map[int]struct{})

	// setup basic info
	c.Options = ops
	Debug(c.Options.Debug)

	if len(c.Options.SharedSecret) < 6 {
		err = fmt.Errorf("code is too short")
		return
	}
	// Create a hash of part of the shared secret to use as the room name
	hashExtra := "croc"
	roomNameBytes := sha256.Sum256([]byte(c.Options.SharedSecret[:4] + hashExtra))
	c.Options.RoomName = hex.EncodeToString(roomNameBytes[:])

	c.conn = make([]*comm.Comm, 16)

	// initialize throttler
	if len(c.Options.ThrottleUpload) > 1 && c.Options.IsSender {
		upload := c.Options.ThrottleUpload[:len(c.Options.ThrottleUpload)-1]
		var uploadLimit int64
		uploadLimit, err = strconv.ParseInt(upload, 10, 64)
		if err != nil {
			panic("Could not parse given Upload Limit")
		}
		minBurstSize := models.TCP_BUFFER_SIZE
		var rt rate.Limit
		switch unit := string(c.Options.ThrottleUpload[len(c.Options.ThrottleUpload)-1:]); unit {
		case "g", "G":
			uploadLimit = uploadLimit * 1024 * 1024 * 1024
		case "m", "M":
			uploadLimit = uploadLimit * 1024 * 1024
		case "k", "K":
			uploadLimit = uploadLimit * 1024
		default:
			uploadLimit, err = strconv.ParseInt(c.Options.ThrottleUpload, 10, 64)
			if err != nil {
				panic("Could not parse given Upload Limit")
			}
		}

		rt = rate.Every(time.Second / time.Duration(uploadLimit))
		if int(uploadLimit) > minBurstSize {
			minBurstSize = int(uploadLimit)
		}
		c.limiter = rate.NewLimiter(rt, minBurstSize)
		log.Debugf("Throttling Upload to %#v", c.limiter.Limit())
	}

	// initialize pake for recipient
	if !c.Options.IsSender {
		c.Pake, err = pake.InitCurve([]byte(c.Options.SharedSecret[5:]), 0, c.Options.Curve)
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

func isEmptyFolder(folderPath string) (bool, error) {
	f, err := os.Open(folderPath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, nil
}

// helper function to walk each subfolder and parses against an ignore file.
// returns a hashmap Key: Absolute filepath, Value: boolean (true=ignore)
func gitWalk(dir string, gitObj *ignore.GitIgnore, files map[string]bool) {
	var ignoredDir bool
	var current string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if isChild(current, path) && ignoredDir {
			files[path] = true
			return nil
		}
		if info.IsDir() && filepath.Base(path) == filepath.Base(dir) {
			ignoredDir = false // Skip applying ignore rules for root directory
			return nil
		}
		if gitObj.MatchesPath(info.Name()) {
			files[path] = true
			ignoredDir = true
			current = path
			return nil
		} else {
			files[path] = false
			ignoredDir = false
			return nil
		}
	})
	if err != nil {
		log.Errorf("filepath error")
	}
}

func isChild(parentPath, childPath string) bool {
	relPath, err := filepath.Rel(parentPath, childPath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(relPath, "..")

}

// This function retrieves the important file information
// for every file that will be transferred
func GetFilesInfo(fnames []string, zipfolder bool, ignoreGit bool) (filesInfo []FileInfo, emptyFolders []FileInfo, totalNumberFolders int, err error) {
	// fnames: the relative/absolute paths of files/folders that will be transferred
	totalNumberFolders = 0
	var paths []string
	for _, fname := range fnames {
		// Support wildcard
		if strings.Contains(fname, "*") {
			matches, errGlob := filepath.Glob(fname)
			if errGlob != nil {
				err = errGlob
				return
			}
			paths = append(paths, matches...)
			continue
		} else {
			paths = append(paths, fname)
		}
	}
	var ignoredPaths = make(map[string]bool)
	if ignoreGit {
		wd, wdErr := os.Stat(".gitignore")
		if wdErr == nil {
			gitIgnore, gitErr := ignore.CompileIgnoreFile(wd.Name())
			if gitErr == nil {
				for _, path := range paths {
					abs, absErr := filepath.Abs(path)
					if absErr != nil {
						err = absErr
						return
					}
					if gitIgnore.MatchesPath(path) {
						ignoredPaths[abs] = true
					}
				}
			}
		}
		for _, path := range paths {
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				err = absErr
				return
			}
			file, fileErr := os.Stat(path)
			if fileErr == nil && file.IsDir() {
				_, subErr := os.Stat(filepath.Join(path, ".gitignore"))
				if subErr == nil {
					gitObj, gitObjErr := ignore.CompileIgnoreFile(filepath.Join(path, ".gitignore"))
					if gitObjErr != nil {
						err = gitObjErr
						return
					}
					gitWalk(abs, gitObj, ignoredPaths)
				}
			}
		}
	}
	for _, fpath := range paths {
		stat, errStat := os.Lstat(fpath)

		if errStat != nil {
			err = errStat
			return
		}

		absPath, errAbs := filepath.Abs(fpath)

		if errAbs != nil {
			err = errAbs
			return
		}
		if stat.IsDir() && zipfolder {
			if fpath[len(fpath)-1:] != "/" {
				fpath += "/"
			}
			fpath = filepath.Dir(fpath)
			dest := filepath.Base(fpath) + ".zip"
			utils.ZipDirectory(dest, fpath)
			utils.MarkFileForRemoval(dest)
			stat, errStat = os.Lstat(dest)
			if errStat != nil {
				err = errStat
				return
			}
			absPath, errAbs = filepath.Abs(dest)
			if errAbs != nil {
				err = errAbs
				return
			}

			fInfo := FileInfo{
				Name:         stat.Name(),
				FolderRemote: "./",
				FolderSource: filepath.Dir(absPath),
				Size:         stat.Size(),
				ModTime:      stat.ModTime(),
				Mode:         stat.Mode(),
				TempFile:     true,
				IsIgnored:    ignoredPaths[absPath],
			}
			if fInfo.IsIgnored {
				continue
			}
			filesInfo = append(filesInfo, fInfo)
			continue
		}

		if stat.IsDir() {
			err = filepath.Walk(absPath,
				func(pathName string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					absPathWithSeparator := filepath.Dir(absPath)
					if !strings.HasSuffix(absPathWithSeparator, string(os.PathSeparator)) {
						absPathWithSeparator += string(os.PathSeparator)
					}
					if strings.HasSuffix(absPathWithSeparator, string(os.PathSeparator)+string(os.PathSeparator)) {
						absPathWithSeparator = strings.TrimSuffix(absPathWithSeparator, string(os.PathSeparator))
					}
					remoteFolder := strings.TrimPrefix(filepath.Dir(pathName), absPathWithSeparator)
					if !info.IsDir() {
						fInfo := FileInfo{
							Name:         info.Name(),
							FolderRemote: strings.ReplaceAll(remoteFolder, string(os.PathSeparator), "/") + "/",
							FolderSource: filepath.Dir(pathName),
							Size:         info.Size(),
							ModTime:      info.ModTime(),
							Mode:         info.Mode(),
							TempFile:     false,
							IsIgnored:    ignoredPaths[pathName],
						}
						if fInfo.IsIgnored && ignoreGit {
							return nil
						} else {
							filesInfo = append(filesInfo, fInfo)
						}
					} else {
						if ignoredPaths[pathName] {
							return filepath.SkipDir
						}
						isEmptyFolder, _ := isEmptyFolder(pathName)
						totalNumberFolders++
						if isEmptyFolder {
							emptyFolders = append(emptyFolders, FileInfo{
								// Name: info.Name(),
								FolderRemote: strings.ReplaceAll(strings.TrimPrefix(pathName,
									filepath.Dir(absPath)+string(os.PathSeparator)), string(os.PathSeparator), "/") + "/",
							})
						}
					}
					return nil
				})
			if err != nil {
				return
			}

		} else {
			fInfo := FileInfo{
				Name:         stat.Name(),
				FolderRemote: "./",
				FolderSource: filepath.Dir(absPath),
				Size:         stat.Size(),
				ModTime:      stat.ModTime(),
				Mode:         stat.Mode(),
				TempFile:     false,
				IsIgnored:    ignoredPaths[absPath],
			}
			if fInfo.IsIgnored && ignoreGit {
				continue
			} else {
				filesInfo = append(filesInfo, fInfo)
			}
		}
	}
	return
}

func (c *Client) sendCollectFiles(filesInfo []FileInfo) (err error) {
	c.FilesToTransfer = filesInfo
	totalFilesSize := int64(0)

	for i, fileInfo := range c.FilesToTransfer {
		var fullPath string
		fullPath = fileInfo.FolderSource + string(os.PathSeparator) + fileInfo.Name
		fullPath = filepath.Clean(fullPath)

		if len(fileInfo.Name) > c.longestFilename {
			c.longestFilename = len(fileInfo.Name)
		}

		if fileInfo.Mode&os.ModeSymlink != 0 {
			log.Debugf("%s is symlink", fileInfo.Name)
			c.FilesToTransfer[i].Symlink, err = os.Readlink(fullPath)
			if err != nil {
				log.Debugf("error getting symlink: %s", err.Error())
			}
			log.Debugf("%+v", c.FilesToTransfer[i])
		}

		if c.Options.HashAlgorithm == "" {
			c.Options.HashAlgorithm = "xxhash"
		}

		c.FilesToTransfer[i].Hash, err = utils.HashFile(fullPath, c.Options.HashAlgorithm, fileInfo.Size > 1e7)
		log.Debugf("hashed %s to %x using %s", fullPath, c.FilesToTransfer[i].Hash, c.Options.HashAlgorithm)
		totalFilesSize += fileInfo.Size
		if err != nil {
			return
		}
		log.Debugf("file %d info: %+v", i, c.FilesToTransfer[i])
		fmt.Fprintf(os.Stderr, "\r                                 ")
		fmt.Fprintf(os.Stderr, "\rSending %d files (%s)", i, utils.ByteCountDecimal(totalFilesSize))
	}
	log.Debugf("longestFilename: %+v", c.longestFilename)
	fname := fmt.Sprintf("%d files", len(c.FilesToTransfer))
	folderName := fmt.Sprintf("%d folders", c.TotalNumberFolders)
	if len(c.FilesToTransfer) == 1 {
		fname = fmt.Sprintf("'%s'", c.FilesToTransfer[0].Name)
	}
	if strings.HasPrefix(fname, "'croc-stdin-") {
		fname = "'stdin'"
		if c.Options.SendingText {
			fname = "'text'"
		}
	}

	fmt.Fprintf(os.Stderr, "\r                                 ")
	if c.TotalNumberFolders > 0 {
		fmt.Fprintf(os.Stderr, "\rSending %s and %s (%s)\n", fname, folderName, utils.ByteCountDecimal(totalFilesSize))
	} else {
		fmt.Fprintf(os.Stderr, "\rSending %s (%s)\n", fname, utils.ByteCountDecimal(totalFilesSize))
	}
	return
}

func (c *Client) setupLocalRelay() {
	// setup the relay locally
	firstPort, _ := strconv.Atoi(c.Options.RelayPorts[0])
	openPorts := utils.FindOpenPorts("127.0.0.1", firstPort, len(c.Options.RelayPorts))
	if len(openPorts) < len(c.Options.RelayPorts) {
		panic("not enough open ports to run local relay")
	}
	for i, port := range openPorts {
		c.Options.RelayPorts[i] = fmt.Sprint(port)
	}
	for _, port := range c.Options.RelayPorts {
		go func(portStr string) {
			debugString := "warn"
			if c.Options.Debug {
				debugString = "debug"
			}
			err := tcp.Run(debugString, "127.0.0.1", portStr, c.Options.RelayPassword, strings.Join(c.Options.RelayPorts[1:], ","))
			if err != nil {
				panic(err)
			}
		}(port)
	}
}

func (c *Client) broadcastOnLocalNetwork(useipv6 bool) {
	var timeLimit time.Duration
	// if we don't use an external relay, the broadcast messages need to be sent continuously
	if c.Options.OnlyLocal {
		timeLimit = -1 * time.Second
	} else {
		timeLimit = 30 * time.Second
	}
	// look for peers first
	settings := peerdiscovery.Settings{
		Limit:     -1,
		Payload:   []byte("croc" + c.Options.RelayPorts[0]),
		Delay:     20 * time.Millisecond,
		TimeLimit: timeLimit,
	}
	if useipv6 {
		settings.IPVersion = peerdiscovery.IPv6
	} else {
		settings.MulticastAddress = "255.255.255.255"
	}

	discoveries, err := peerdiscovery.Discover(settings)
	log.Debugf("discoveries: %+v", discoveries)

	if err != nil {
		log.Debug(err)
	}
}

func (c *Client) transferOverLocalRelay(errchan chan<- error) {
	time.Sleep(500 * time.Millisecond)
	log.Debug("establishing connection")
	var banner string
	conn, banner, ipaddr, err := tcp.ConnectToTCPServer("127.0.0.1:"+c.Options.RelayPorts[0], c.Options.RelayPassword, c.Options.RoomName)
	log.Debugf("banner: %s", banner)
	if err != nil {
		err = fmt.Errorf("could not connect to 127.0.0.1:%s: %w", c.Options.RelayPorts[0], err)
		log.Debug(err)
		// not really an error because it will try to connect over the actual relay
		return
	}
	log.Debugf("local connection established: %+v", conn)
	for {
		data, _ := conn.Receive()
		if bytes.Equal(data, handshakeRequest) {
			break
		} else if bytes.Equal(data, []byte{1}) {
			log.Debug("got ping")
		} else {
			log.Debugf("instead of handshake got: %s", data)
		}
	}
	c.conn[0] = conn
	log.Debug("exchanged header message")
	c.Options.RelayAddress = "127.0.0.1"
	c.Options.RelayPorts = strings.Split(banner, ",")
	if c.Options.NoMultiplexing {
		log.Debug("no multiplexing")
		c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
	}
	c.ExternalIP = ipaddr
	errchan <- c.transfer()
}

// Send will send the specified file
func (c *Client) Send(filesInfo []FileInfo, emptyFoldersToTransfer []FileInfo, totalNumberFolders int) (err error) {
	c.EmptyFoldersToTransfer = emptyFoldersToTransfer
	c.TotalNumberFolders = totalNumberFolders
	c.TotalNumberOfContents = len(filesInfo)
	err = c.sendCollectFiles(filesInfo)
	if err != nil {
		return
	}
	flags := &strings.Builder{}
	if c.Options.RelayAddress != models.DEFAULT_RELAY && !c.Options.OnlyLocal {
		flags.WriteString("--relay " + c.Options.RelayAddress + " ")
	}
	if c.Options.RelayPassword != models.DEFAULT_PASSPHRASE {
		flags.WriteString("--pass " + c.Options.RelayPassword + " ")
	}
	fmt.Fprintf(os.Stderr, `Code is: %[1]s

On the other computer run:
(For Windows)
    croc %[2]s%[1]s
(For Linux/OSX)
    CROC_SECRET=%[1]q croc %[2]s
`, c.Options.SharedSecret, flags.String())
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
		// broadcast on ipv4
		go c.broadcastOnLocalNetwork(false)
		// broadcast on ipv6
		go c.broadcastOnLocalNetwork(true)
		go c.transferOverLocalRelay(errchan)
	}

	if !c.Options.OnlyLocal {
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
					port = models.DEFAULT_PORT
				}
				log.Debugf("got host '%v' and port '%v'", host, port)
				address = net.JoinHostPort(host, port)
				log.Debugf("trying connection to %s", address)
				conn, banner, ipaddr, err = tcp.ConnectToTCPServer(address, c.Options.RelayPassword, c.Options.RoomName, durations[i])
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
			var kB []byte
			B, _ := pake.InitCurve([]byte(c.Options.SharedSecret[5:]), 1, c.Options.Curve)
			for {
				var dataMessage SimpleMessage
				log.Trace("waiting for bytes")
				data, errConn := conn.Receive()
				if errConn != nil {
					log.Tracef("[%+v] had error: %s", conn, errConn.Error())
				}
				json.Unmarshal(data, &dataMessage)
				log.Tracef("data: %+v '%s'", data, data)
				log.Tracef("dataMessage: %s", dataMessage)
				log.Tracef("kB: %x", kB)
				// if kB not null, then use it to decrypt
				if kB != nil {
					var decryptErr error
					var dataDecrypt []byte
					dataDecrypt, decryptErr = crypt.Decrypt(data, kB)
					if decryptErr != nil {
						log.Tracef("error decrypting: %v: '%s'", decryptErr, data)
					} else {
						// copy dataDecrypt to data
						data = dataDecrypt
						log.Tracef("decrypted: %s", data)
					}
				}
				if bytes.Equal(data, ipRequest) {
					log.Tracef("got ipRequest")
					// recipient wants to try to connect to local ips
					var ips []string
					// only get local ips if the local is enabled
					if !c.Options.DisableLocal {
						// get list of local ips
						ips, err = utils.GetLocalIPs()
						if err != nil {
							log.Tracef("error getting local ips: %v", err)
						}
						// prepend the port that is being listened to
						ips = append([]string{c.Options.RelayPorts[0]}, ips...)
					}
					log.Tracef("sending ips: %+v", ips)
					bips, errIps := json.Marshal(ips)
					if errIps != nil {
						log.Tracef("error marshalling ips: %v", errIps)
					}
					bips, errIps = crypt.Encrypt(bips, kB)
					if errIps != nil {
						log.Tracef("error encrypting ips: %v", errIps)
					}
					if err = conn.Send(bips); err != nil {
						log.Errorf("error sending: %v", err)
					}
				} else if dataMessage.Kind == "pake1" {
					log.Trace("got pake1")
					var pakeError error
					pakeError = B.Update(dataMessage.Bytes)
					if pakeError == nil {
						kB, pakeError = B.SessionKey()
						if pakeError == nil {
							log.Tracef("dataMessage kB: %x", kB)
							dataMessage.Bytes = B.Bytes()
							dataMessage.Kind = "pake2"
							data, _ = json.Marshal(dataMessage)
							if pakeError = conn.Send(data); err != nil {
								log.Errorf("dataMessage error sending: %v", err)
							}
						}

					}
				} else if bytes.Equal(data, handshakeRequest) {
					log.Trace("got handshake")
					break
				} else if bytes.Equal(data, []byte{1}) {
					log.Trace("got ping")
					continue
				} else {
					log.Tracef("[%+v] got weird bytes: %+v", conn, data)
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
			errchan <- c.transfer()
		}()
	}

	err = <-errchan
	if err == nil {
		// return if no error
		return
	} else {
		log.Debugf("error from errchan: %v", err)
		if strings.Contains(err.Error(), "could not secure channel") {
			return err
		}
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
	isIPset := false

	if c.Options.OnlyLocal || c.Options.IP != "" {
		c.Options.RelayAddress = ""
		c.Options.RelayAddress6 = ""
	}

	if c.Options.IP != "" {
		// check ip version
		if strings.Count(c.Options.IP, ":") >= 2 {
			log.Debug("assume ipv6")
			c.Options.RelayAddress6 = c.Options.IP
		}
		if strings.Contains(c.Options.IP, ".") {
			log.Debug("assume ipv4")
			c.Options.RelayAddress = c.Options.IP
		}
		isIPset = true
	}

	if !c.Options.DisableLocal && !isIPset {
		log.Debug("attempt to discover peers")
		var discoveries []peerdiscovery.Discovered
		var wgDiscovery sync.WaitGroup
		var dmux sync.Mutex
		wgDiscovery.Add(2)
		go func() {
			defer wgDiscovery.Done()
			ipv4discoveries, err1 := peerdiscovery.Discover(peerdiscovery.Settings{
				Limit:            1,
				Payload:          []byte("ok"),
				Delay:            20 * time.Millisecond,
				TimeLimit:        200 * time.Millisecond,
				MulticastAddress: "255.255.255.255",
			})
			if err1 == nil && len(ipv4discoveries) > 0 {
				dmux.Lock()
				err = err1
				discoveries = append(discoveries, ipv4discoveries...)
				dmux.Unlock()
			}
		}()
		go func() {
			defer wgDiscovery.Done()
			ipv6discoveries, err1 := peerdiscovery.Discover(peerdiscovery.Settings{
				Limit:     1,
				Payload:   []byte("ok"),
				Delay:     20 * time.Millisecond,
				TimeLimit: 200 * time.Millisecond,
				IPVersion: peerdiscovery.IPv6,
			})
			if err1 == nil && len(ipv6discoveries) > 0 {
				dmux.Lock()
				err = err1
				discoveries = append(discoveries, ipv6discoveries...)
				dmux.Unlock()
			}
		}()
		wgDiscovery.Wait()

		if err == nil && len(discoveries) > 0 {
			log.Debugf("all discoveries: %+v", discoveries)
			for i := 0; i < len(discoveries); i++ {
				log.Debugf("discovery %d has payload: %+v", i, discoveries[i])
				if !bytes.HasPrefix(discoveries[i].Payload, []byte("croc")) {
					log.Debug("skipping discovery")
					continue
				}
				log.Debug("switching to local")
				portToUse := string(bytes.TrimPrefix(discoveries[i].Payload, []byte("croc")))
				if portToUse == "" {
					portToUse = models.DEFAULT_PORT
				}
				address := net.JoinHostPort(discoveries[i].Address, portToUse)
				errPing := tcp.PingServer(address)
				if errPing == nil {
					log.Debugf("successfully pinged '%s'", address)
					c.Options.RelayAddress = address
					c.ExternalIPConnected = c.Options.RelayAddress
					c.Options.RelayAddress6 = ""
					usingLocal = true
					break
				} else {
					log.Debugf("could not ping: %+v", errPing)
				}
			}
		}
		log.Debugf("discoveries: %+v", discoveries)
		log.Debug("establishing connection")
	}
	var banner string
	durations := []time.Duration{200 * time.Millisecond, 5 * time.Second}
	err = fmt.Errorf("found no addresses to connect")
	for i, address := range []string{c.Options.RelayAddress6, c.Options.RelayAddress} {
		if address == "" {
			continue
		}
		var host, port string
		host, port, _ = net.SplitHostPort(address)
		// Default port to :9009
		if port == "" {
			host = address
			port = models.DEFAULT_PORT
		}
		log.Debugf("got host '%v' and port '%v'", host, port)
		address = net.JoinHostPort(host, port)
		log.Debugf("trying connection to %s", address)
		c.conn[0], banner, c.ExternalIP, err = tcp.ConnectToTCPServer(address, c.Options.RelayPassword, c.Options.RoomName, durations[i])
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

	if c.Options.TestFlag {
		log.Debugf("TEST FLAG ENABLED, TESTING LOCAL IPS")
	}
	if c.Options.TestFlag || (!usingLocal && !c.Options.DisableLocal && !isIPset) {
		// ask the sender for their local ips and port
		// and try to connect to them

		var ips []string
		err = func() (err error) {
			var A *pake.Pake
			var data []byte
			A, err = pake.InitCurve([]byte(c.Options.SharedSecret[5:]), 0, c.Options.Curve)
			if err != nil {
				return err
			}
			dataMessage := SimpleMessage{
				Bytes: A.Bytes(),
				Kind:  "pake1",
			}
			data, _ = json.Marshal(dataMessage)
			if err = c.conn[0].Send(data); err != nil {
				log.Errorf("dataMessage send error: %v", err)
				return
			}
			data, err = c.conn[0].Receive()
			if err != nil {
				return
			}
			err = json.Unmarshal(data, &dataMessage)
			if err != nil || dataMessage.Kind != "pake2" {
				log.Debugf("data: %s", data)
				return fmt.Errorf("dataMessage %s pake failed", ipRequest)
			}
			err = A.Update(dataMessage.Bytes)
			if err != nil {
				return
			}
			var kA []byte
			kA, err = A.SessionKey()
			if err != nil {
				return
			}
			log.Debugf("dataMessage kA: %x", kA)

			// secure ipRequest
			data, err = crypt.Encrypt([]byte(ipRequest), kA)
			if err != nil {
				return
			}
			log.Debug("sending ips?")
			if err = c.conn[0].Send(data); err != nil {
				log.Errorf("ips send error: %v", err)
			}
			data, err = c.conn[0].Receive()
			if err != nil {
				return
			}
			data, err = crypt.Decrypt(data, kA)
			if err != nil {
				return
			}
			log.Debugf("ips data: %s", data)
			if err = json.Unmarshal(data, &ips); err != nil {
				log.Debugf("ips unmarshal error: %v", err)
			}
			return
		}()

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
					log.Debugf("localIP: %+v, localIPparsed: %+v", localIP, localIPparsed)
					if ipv4Net.Contains(localIPparsed) {
						haveLocalIP = true
						log.Debugf("ip: %+v is a local IP", ip)
						break
					}
				}
				if !haveLocalIP {
					log.Debugf("%s is not a local IP, skipping", ip)
					continue
				}

				serverTry := net.JoinHostPort(ip, port)
				conn, banner2, externalIP, errConn := tcp.ConnectToTCPServer(serverTry, c.Options.RelayPassword, c.Options.RoomName, 500*time.Millisecond)
				if errConn != nil {
					log.Debug(errConn)
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

	if err = c.conn[0].Send(handshakeRequest); err != nil {
		log.Errorf("handshake send error: %v", err)
	}
	c.Options.RelayPorts = strings.Split(banner, ",")
	if c.Options.NoMultiplexing {
		log.Debug("no multiplexing")
		c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
	}
	log.Debug("exchanged header message")
	fmt.Fprintf(os.Stderr, "\rsecuring channel...")
	err = c.transfer()
	if err == nil {
		if c.numberOfTransferredFiles+len(c.EmptyFoldersToTransfer) == 0 {
			fmt.Fprintf(os.Stderr, "\rNo files transferred.\n")
		}
	}
	return
}

func (c *Client) transfer() (err error) {
	// connect to the server

	// quit with c.quit <- true
	c.quit = make(chan bool)

	// if recipient, initialize with sending pake information
	log.Debug("ready")
	if !c.Options.IsSender && !c.Step1ChannelSecured {
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:   message.TypePAKE,
			Bytes:  c.Pake.Bytes(),
			Bytes2: []byte(c.Options.Curve),
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
			log.Debugf("data: %s", data)
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
	if c.Options.IsSender && c.SuccessfulTransfer {
		for _, file := range c.FilesToTransfer {
			if file.TempFile {
				fmt.Println("Removing " + file.Name)
				os.Remove(file.Name)
			}
		}
	}

	if c.SuccessfulTransfer && !c.Options.IsSender {
		for _, file := range c.FilesToTransfer {
			if file.TempFile {
				utils.UnzipDirectory(".", file.Name)
				os.Remove(file.Name)
				log.Debugf("Removing %s\n", file.Name)
			}
		}
	}

	if c.Options.Stdout && !c.Options.IsSender {
		pathToFile := path.Join(
			c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote,
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
		)
		log.Debugf("pathToFile: %s", pathToFile)
		// close if not closed already
		if !c.CurrentFileIsClosed {
			c.CurrentFile.Close()
			c.CurrentFileIsClosed = true
		}
		if err = os.Remove(pathToFile); err != nil {
			log.Warnf("error removing %s: %v", pathToFile, err)
		}
		fmt.Fprint(os.Stderr, "\n")
	}
	if err != nil && strings.Contains(err.Error(), "pake not successful") {
		log.Debugf("pake error: %s", err.Error())
		err = fmt.Errorf("password mismatch")
	}
	if err != nil && strings.Contains(err.Error(), "unexpected end of JSON input") {
		log.Debugf("error: %s", err.Error())
		err = fmt.Errorf("room (secure channel) not ready, maybe peer disconnected")
	}
	return
}

func (c *Client) createEmptyFolder(i int) (err error) {
	err = os.MkdirAll(c.EmptyFoldersToTransfer[i].FolderRemote, os.ModePerm)
	if err != nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", c.EmptyFoldersToTransfer[i].FolderRemote)
	c.bar = progressbar.NewOptions64(1,
		progressbar.OptionOnCompletion(func() {
			c.fmtPrintUpdate()
		}),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription(" "),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetVisibility(!c.Options.SendingText),
	)
	c.bar.Finish()
	return
}

func (c *Client) processMessageFileInfo(m message.Message) (done bool, err error) {
	var senderInfo SenderInfo
	err = json.Unmarshal(m.Bytes, &senderInfo)
	if err != nil {
		log.Debug(err)
		return
	}
	c.Options.SendingText = senderInfo.SendingText
	c.Options.NoCompress = senderInfo.NoCompress
	c.Options.HashAlgorithm = senderInfo.HashAlgorithm
	c.EmptyFoldersToTransfer = senderInfo.EmptyFoldersToTransfer
	c.TotalNumberFolders = senderInfo.TotalNumberFolders
	c.FilesToTransfer = senderInfo.FilesToTransfer
	for i, fi := range c.FilesToTransfer {
		// Issues #593 - sanitize the sender paths and prevent ".." from being used
		c.FilesToTransfer[i].FolderRemote = filepath.Clean(fi.FolderRemote)
		if strings.Contains(c.FilesToTransfer[i].FolderRemote, "..") {
			return true, fmt.Errorf("invalid path detected: '%s'", fi.FolderRemote)
		}
		// Issues #593 - disallow specific folders like .ssh
		if strings.Contains(c.FilesToTransfer[i].FolderRemote, ".ssh") {
			return true, fmt.Errorf("invalid path detected: '%s'", fi.FolderRemote)
		}
		// Issue #595 - disallow filenames with invisible characters
		errFileName := utils.ValidFileName(path.Join(c.FilesToTransfer[i].FolderRemote, fi.Name))
		if errFileName != nil {
			return true, errFileName
		}
	}
	c.TotalNumberOfContents = 0
	if c.FilesToTransfer != nil {
		c.TotalNumberOfContents += len(c.FilesToTransfer)
	}
	if c.EmptyFoldersToTransfer != nil {
		c.TotalNumberOfContents += len(c.EmptyFoldersToTransfer)
	}

	if c.Options.HashAlgorithm == "" {
		c.Options.HashAlgorithm = "xxhash"
	}
	log.Debugf("using hash algorithm: %s", c.Options.HashAlgorithm)
	if c.Options.NoCompress {
		log.Debug("disabling compression")
	}
	if c.Options.SendingText {
		c.Options.Stdout = true
	}

	fname := fmt.Sprintf("%d files", len(c.FilesToTransfer))
	folderName := fmt.Sprintf("%d folders", c.TotalNumberFolders)
	if len(c.FilesToTransfer) == 1 {
		fname = fmt.Sprintf("'%s'", c.FilesToTransfer[0].Name)
	}
	totalSize := int64(0)
	for i, fi := range c.FilesToTransfer {
		totalSize += fi.Size
		if len(fi.Name) > c.longestFilename {
			c.longestFilename = len(fi.Name)
		}
		if strings.HasPrefix(fi.Name, "croc-stdin-") && c.Options.SendingText {
			c.FilesToTransfer[i].Name, err = utils.RandomFileName()
			if err != nil {
				return
			}
		}
	}
	// // check the totalSize does not exceed disk space
	// usage := diskusage.NewDiskUsage(".")
	// if usage.Available() < uint64(totalSize) {
	// 	return true, fmt.Errorf("not enough disk space")
	// }

	// c.spinner.Stop()
	action := "Accept"
	if c.Options.SendingText {
		action = "Display"
		fname = "text message"
	}
	if !c.Options.NoPrompt || c.Options.Ask || senderInfo.Ask {
		if c.Options.Ask || senderInfo.Ask {
			machID, _ := machineid.ID()
			fmt.Fprintf(os.Stderr, "\rYour machine id is '%s'.\n%s %s (%s) from '%s'? (Y/n) ", machID, action, fname, utils.ByteCountDecimal(totalSize), senderInfo.MachineID)
		} else {
			if c.TotalNumberFolders > 0 {
				fmt.Fprintf(os.Stderr, "\r%s %s and %s (%s)? (Y/n) ", action, fname, folderName, utils.ByteCountDecimal(totalSize))
			} else {
				fmt.Fprintf(os.Stderr, "\r%s %s (%s)? (Y/n) ", action, fname, utils.ByteCountDecimal(totalSize))
			}
		}
		choice := strings.ToLower(utils.GetInput(""))
		if choice != "" && choice != "y" && choice != "yes" {
			err = message.Send(c.conn[0], c.Key, message.Message{
				Type:    message.TypeError,
				Message: "refusing files",
			})
			if err != nil {
				return false, err
			}
			return true, fmt.Errorf("refused files")
		}
	} else {
		fmt.Fprintf(os.Stderr, "\rReceiving %s (%s) \n", fname, utils.ByteCountDecimal(totalSize))
	}
	fmt.Fprintf(os.Stderr, "\nReceiving (<-%s)\n", c.ExternalIPConnected)

	for i := 0; i < len(c.EmptyFoldersToTransfer); i += 1 {
		_, errExists := os.Stat(c.EmptyFoldersToTransfer[i].FolderRemote)
		if os.IsNotExist(errExists) {
			err = c.createEmptyFolder(i)
			if err != nil {
				return
			}
		} else {
			isEmpty, _ := isEmptyFolder(c.EmptyFoldersToTransfer[i].FolderRemote)
			if !isEmpty {
				log.Debug("asking to overwrite")
				prompt := fmt.Sprintf("\n%s already has some content in it. \nDo you want"+
					" to overwrite it with an empty folder? (y/N) ", c.EmptyFoldersToTransfer[i].FolderRemote)
				choice := strings.ToLower(utils.GetInput(prompt))
				if choice == "y" || choice == "yes" {
					err = c.createEmptyFolder(i)
					if err != nil {
						return
					}
				}
			}
		}
	}

	// if no files are to be transferred, then we can end the file transfer process
	if c.FilesToTransfer == nil {
		c.SuccessfulTransfer = true
		c.Step3RecipientRequestFile = true
		c.Step4FileTransferred = true
		errStopTransfer := message.Send(c.conn[0], c.Key, message.Message{
			Type: message.TypeFinished,
		})
		if errStopTransfer != nil {
			err = errStopTransfer
		}
	}
	log.Debug(c.FilesToTransfer)
	c.Step2FileInfoTransferred = true
	return
}

func (c *Client) processMessagePake(m message.Message) (err error) {
	log.Debug("received pake payload")

	var salt []byte
	if c.Options.IsSender {
		// initialize curve based on the recipient's choice
		log.Debugf("using curve %s", string(m.Bytes2))
		c.Pake, err = pake.InitCurve([]byte(c.Options.SharedSecret[5:]), 1, string(m.Bytes2))
		if err != nil {
			log.Error(err)
			return
		}

		// update the pake
		err = c.Pake.Update(m.Bytes)
		if err != nil {
			return
		}

		// generate salt and send it back to recipient
		log.Debug("generating salt")
		salt = make([]byte, 8)
		if _, rerr := rand.Read(salt); err != nil {
			log.Errorf("can't generate random numbers: %v", rerr)
			return
		}
		log.Debug("sender sending pake+salt")
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:   message.TypePAKE,
			Bytes:  c.Pake.Bytes(),
			Bytes2: salt,
		})
	} else {
		err = c.Pake.Update(m.Bytes)
		if err != nil {
			return
		}
		salt = m.Bytes2
	}
	// generate key
	key, err := c.Pake.SessionKey()
	if err != nil {
		return err
	}
	c.Key, _, err = crypt.New(key, salt)
	if err != nil {
		return err
	}
	log.Debugf("generated key = %+x with salt %x", c.Key, salt)

	// connects to the other ports of the server for transfer
	var wg sync.WaitGroup
	wg.Add(len(c.Options.RelayPorts))
	for i := 0; i < len(c.Options.RelayPorts); i++ {
		log.Debugf("port: [%s]", c.Options.RelayPorts[i])
		go func(j int) {
			defer wg.Done()
			var host string
			if c.Options.RelayAddress == "127.0.0.1" {
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
				fmt.Sprintf("%s-%d", c.Options.RoomName, j),
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

	if !c.Options.IsSender {
		log.Debug("sending external IP")
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:    message.TypeExternalIP,
			Message: c.ExternalIP,
			Bytes:   m.Bytes,
		})
	}
	return
}

func (c *Client) processExternalIP(m message.Message) (done bool, err error) {
	log.Debugf("received external IP: %+v", m)
	if c.Options.IsSender {
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:    message.TypeExternalIP,
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

	// only "pake" messages should be unencrypted
	// if a non-"pake" message is received unencrypted something
	// is weird
	if m.Type != message.TypePAKE && c.Key == nil {
		err = fmt.Errorf("unencrypted communication rejected")
		done = true
		return
	}

	switch m.Type {
	case message.TypeFinished:
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type: message.TypeFinished,
		})
		done = true
		c.SuccessfulTransfer = true
		return
	case message.TypePAKE:
		err = c.processMessagePake(m)
		if err != nil {
			err = fmt.Errorf("pake not successful: %w", err)
			log.Debug(err)
		}
	case message.TypeExternalIP:
		done, err = c.processExternalIP(m)
	case message.TypeError:
		// c.spinner.Stop()
		fmt.Print("\r")
		err = fmt.Errorf("peer error: %s", m.Message)
		return true, err
	case message.TypeFileInfo:
		done, err = c.processMessageFileInfo(m)
	case message.TypeRecipientReady:
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
			fmt.Fprintf(os.Stderr, "Send to machine '%s'? (Y/n) ", remoteFile.MachineID)
			choice := strings.ToLower(utils.GetInput(""))
			if choice != "" && choice != "y" && choice != "yes" {
				err = message.Send(c.conn[0], c.Key, message.Message{
					Type:    message.TypeError,
					Message: "refusing files",
				})
				done = true
				return
			}
		}
	case message.TypeCloseSender:
		c.bar.Finish()
		log.Debug("close-sender received...")
		c.Step4FileTransferred = false
		c.Step3RecipientRequestFile = false
		log.Debug("sending close-recipient")
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type: message.TypeCloseRecipient,
		})
	case message.TypeCloseRecipient:
		c.Step4FileTransferred = false
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
	if c.Options.IsSender && c.Step1ChannelSecured && !c.Step2FileInfoTransferred {
		var b []byte
		machID, _ := machineid.ID()
		b, err = json.Marshal(SenderInfo{
			FilesToTransfer:        c.FilesToTransfer,
			EmptyFoldersToTransfer: c.EmptyFoldersToTransfer,
			MachineID:              machID,
			Ask:                    c.Options.Ask,
			TotalNumberFolders:     c.TotalNumberFolders,
			SendingText:            c.Options.SendingText,
			NoCompress:             c.Options.NoCompress,
			HashAlgorithm:          c.Options.HashAlgorithm,
		})
		if err != nil {
			log.Error(err)
			return
		}
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:  message.TypeFileInfo,
			Bytes: b,
		})
		if err != nil {
			return
		}

		c.Step2FileInfoTransferred = true
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
		os.O_WRONLY, 0o666)
	var truncate bool // default false
	c.CurrentFileChunks = []int64{}
	c.CurrentFileChunkRanges = []int64{}
	if errOpen == nil {
		stat, _ := c.CurrentFile.Stat()
		truncate = stat.Size() != c.FilesToTransfer[c.FilesToTransferCurrentNum].Size
		if !truncate {
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
		errChmod := os.Chmod(pathToFile, c.FilesToTransfer[c.FilesToTransferCurrentNum].Mode.Perm())
		if errChmod != nil {
			log.Error(errChmod)
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
			Type: message.TypeFinished,
		})
		if err != nil {
			panic(err)
		}
		c.SuccessfulTransfer = true
		c.FilesHasFinished[c.FilesToTransferCurrentNum] = struct{}{}
		return
	}

	err = c.recipientInitializeFile()
	if err != nil {
		return
	}

	c.TotalSent = 0
	c.CurrentFileIsClosed = false
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
		Type:  message.TypeRecipientReady,
		Bytes: bRequest,
	})
	if err != nil {
		return
	}
	c.Step3RecipientRequestFile = true
	return
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func formatDescription(description string) string {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	width = max(20, width-60)
	if err != nil {
		return description
	}
	if len(description) > width {
		description = description[:(width-3)] + "..."
	}
	return description
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
	pathToFile := path.Join(fileInfo.FolderRemote, fileInfo.Name)
	if fileInfo.Symlink != "" {
		log.Debug("creating symlink")
		// remove symlink if it exists
		if _, errExists := os.Lstat(pathToFile); errExists == nil {
			os.Remove(pathToFile)
		}
		err = os.Symlink(fileInfo.Symlink, pathToFile)
		if err != nil {
			return
		}
	} else {
		emptyFile, errCreate := os.Create(pathToFile)
		if errCreate != nil {
			log.Error(errCreate)
			err = errCreate
			return
		}
		emptyFile.Close()
	}
	// setup the progressbar
	description := fmt.Sprintf("%-*s", c.longestFilename, c.FilesToTransfer[i].Name)
	if len(c.FilesToTransfer) == 1 {
		description = c.FilesToTransfer[i].Name
		// description = ""
	} else {
		description = " " + description
	}
	c.bar = progressbar.NewOptions64(1,
		progressbar.OptionOnCompletion(func() {
			c.fmtPrintUpdate()
		}),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription(formatDescription(description)),
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
	if c.Options.IsSender || !c.Step2FileInfoTransferred || c.Step3RecipientRequestFile {
		return
	}
	// find the next file to transfer and send that number
	// if the files are the same size, then look for missing chunks
	finished := true
	for i, fileInfo := range c.FilesToTransfer {
		if _, ok := c.FilesHasFinished[i]; ok {
			continue
		}
		if i < c.FilesToTransferCurrentNum {
			continue
		}
		log.Debugf("checking %+v", fileInfo)
		recipientFileInfo, errRecipientFile := os.Lstat(path.Join(fileInfo.FolderRemote, fileInfo.Name))
		var errHash error
		var fileHash []byte
		if errRecipientFile == nil && recipientFileInfo.Size() == fileInfo.Size {
			// the file exists, but is same size, so hash it
			fileHash, errHash = utils.HashFile(path.Join(fileInfo.FolderRemote, fileInfo.Name), c.Options.HashAlgorithm)
		}
		if fileInfo.Size == 0 || fileInfo.Symlink != "" {
			err = c.createEmptyFileAndFinish(fileInfo, i)
			if err != nil {
				return
			} else {
				c.numberOfTransferredFiles++
			}
			continue
		}
		log.Debugf("%s %+x %+x %+v", fileInfo.Name, fileHash, fileInfo.Hash, errHash)
		if !bytes.Equal(fileHash, fileInfo.Hash) {
			log.Debugf("hashed %s to %x using %s", fileInfo.Name, fileHash, c.Options.HashAlgorithm)
			log.Debugf("hashes are not equal %x != %x", fileHash, fileInfo.Hash)
			if errHash == nil && !c.Options.Overwrite && errRecipientFile == nil && !strings.HasPrefix(fileInfo.Name, "croc-stdin-") && !c.Options.SendingText {

				missingChunks := utils.ChunkRangesToChunks(utils.MissingChunks(
					path.Join(fileInfo.FolderRemote, fileInfo.Name),
					fileInfo.Size,
					models.TCP_BUFFER_SIZE/2,
				))
				percentDone := 100 - float64(len(missingChunks)*models.TCP_BUFFER_SIZE/2)/float64(fileInfo.Size)*100

				log.Debug("asking to overwrite")
				prompt := fmt.Sprintf("\nOverwrite '%s'? (y/N) (use --overwrite to omit) ", path.Join(fileInfo.FolderRemote, fileInfo.Name))
				if percentDone < 99 {
					prompt = fmt.Sprintf("\nResume '%s' (%2.1f%%)? (y/N)   (use --overwrite to omit) ", path.Join(fileInfo.FolderRemote, fileInfo.Name), percentDone)
				}
				choice := strings.ToLower(utils.GetInput(prompt))
				if choice != "y" && choice != "yes" {
					fmt.Fprintf(os.Stderr, "Skipping '%s'\n", path.Join(fileInfo.FolderRemote, fileInfo.Name))
					continue
				}
			}
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
			c.numberOfTransferredFiles++
			newFolder, _ := filepath.Split(fileInfo.FolderRemote)
			if newFolder != c.LastFolder && len(c.FilesToTransfer) > 0 && !c.Options.SendingText && newFolder != "./" {
				fmt.Fprintf(os.Stderr, "\r%s\n", newFolder)
			}
			c.LastFolder = newFolder
			break
		}
	}
	c.recipientGetFileReady(finished)
	return
}

func (c *Client) fmtPrintUpdate() {
	c.finishedNum++
	if c.TotalNumberOfContents > 1 {
		fmt.Fprintf(os.Stderr, " %d/%d\n", c.finishedNum, c.TotalNumberOfContents)
	} else {
		fmt.Fprintf(os.Stderr, "\n")
	}
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

	if c.Options.IsSender && c.Step3RecipientRequestFile && !c.Step4FileTransferred {
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
						// description = ""
					}

					c.bar = progressbar.NewOptions64(1,
						progressbar.OptionOnCompletion(func() {
							c.fmtPrintUpdate()
						}),
						progressbar.OptionSetWidth(20),
						progressbar.OptionSetDescription(formatDescription(description)),
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
		c.Step4FileTransferred = true
		// setup the progressbar
		c.setBar()
		c.TotalSent = 0
		c.CurrentFileIsClosed = false
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
	folder, _ := filepath.Split(c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote)
	if folder == "./" {
		description = c.FilesToTransfer[c.FilesToTransferCurrentNum].Name
	} else if !c.Options.IsSender {
		description = " " + description
	}
	c.bar = progressbar.NewOptions64(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		progressbar.OptionOnCompletion(func() {
			c.fmtPrintUpdate()
		}),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription(formatDescription(description)),
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
	log.Tracef("%d receiving data", i)
	for {
		data, err := c.conn[i+1].Receive()
		if err != nil {
			break
		}
		if bytes.Equal(data, []byte{1}) {
			log.Trace("got ping")
			continue
		}

		data, err = crypt.Decrypt(data, c.Key)
		if err != nil {
			panic(err)
		}
		if !c.Options.NoCompress {
			data = compress.Decompress(data)
		}

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
		if err != nil {
			panic(err)
		}
		c.bar.Add(len(data[8:]))
		c.TotalSent += int64(len(data[8:]))
		c.TotalChunksTransferred++
		// log.Debug(len(c.CurrentFileChunks), c.TotalChunksTransferred, c.TotalSent, c.FilesToTransfer[c.FilesToTransferCurrentNum].Size)

		if !c.CurrentFileIsClosed && (c.TotalChunksTransferred == len(c.CurrentFileChunks) || c.TotalSent == c.FilesToTransfer[c.FilesToTransferCurrentNum].Size) {
			c.CurrentFileIsClosed = true
			log.Debug("finished receiving!")
			if err = c.CurrentFile.Close(); err != nil {
				log.Debugf("error closing %s: %v", c.CurrentFile.Name(), err)
			} else {
				log.Debugf("Successful closing %s", c.CurrentFile.Name())
			}
			if c.Options.Stdout || c.Options.SendingText {
				pathToFile := path.Join(
					c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote,
					c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
				)
				b, _ := os.ReadFile(pathToFile)
				fmt.Print(string(b))
			}
			log.Debug("sending close-sender")
			err = message.Send(c.conn[0], c.Key, message.Message{
				Type: message.TypeCloseSender,
			})
			if err != nil {
				panic(err)
			}
		}
		c.mutex.Unlock()
	}
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
		var n int
		var errRead error
		if math.Mod(curi, float64(len(c.Options.RelayPorts))) == float64(i) {
			data := make([]byte, models.TCP_BUFFER_SIZE/2)
			n, errRead = c.fread.ReadAt(data, readingPos)
			if c.limiter != nil {
				r := c.limiter.ReserveN(time.Now(), n)
				log.Debugf("Limiting Upload for %d", r.Delay())
				time.Sleep(r.Delay())
			}
			if n > 0 {
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
					var err error
					var dataToSend []byte
					if c.Options.NoCompress {
						dataToSend, err = crypt.Encrypt(
							append(posByte, data[:n]...),
							c.Key,
						)
					} else {
						dataToSend, err = crypt.Encrypt(
							compress.Compress(
								append(posByte, data[:n]...),
							),
							c.Key,
						)
					}
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
				}
			}
		}

		if n == 0 {
			n = models.TCP_BUFFER_SIZE / 2
		}
		readingPos += int64(n)
		curi++
		pos += uint64(n)

		if errRead != nil {
			if errRead == io.EOF {
				break
			}
			panic(errRead)
		}
	}
}
