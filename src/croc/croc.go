package croc

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/denisbrodbeck/machineid"
	ignore "github.com/sabhiram/go-gitignore"
	log "github.com/schollz/logger"
	"github.com/schollz/pake/v3"
	"github.com/schollz/peerdiscovery"
	"github.com/schollz/progressbar/v3"
	"github.com/skip2/go-qrcode"
	"golang.org/x/term"
	"golang.org/x/time/rate"

	"github.com/schollz/croc/v11/src/codephrase"
	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/compress"
	"github.com/schollz/croc/v11/src/crypt"
	"github.com/schollz/croc/v11/src/message"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/pakekey"
	"github.com/schollz/croc/v11/src/tcp"
	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/croc/v11/src/utils"
)

var (
	ipRequest        = []byte("ips?")
	handshakeRequest = []byte("handshake")

	alternateSenderRouteTimeout = 10 * time.Second
)

const (
	ReconnectVersion                   = 1
	maxReconnectAttempts               = 10
	reconnectCandidateHandshakeTimeout = 2 * time.Second
	maxDecompressedChunkSize           = models.TCP_BUFFER_SIZE/2 + 8
)

func init() {
	log.SetLevel("debug")
	log.SetOutput(termui.LoggerOutput(os.Stdout))
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
	IsSender          bool
	SharedSecret      string
	RoomName          string
	Debug             bool
	RelayAddress      string
	RelayAddress6     string
	PublicRelay       bool
	RelayPorts        []string
	RelayPassword     string
	Stdout            bool
	NoPrompt          bool
	NoMultiplexing    bool
	DisableLocal      bool
	OnlyLocal         bool
	IgnoreStdin       bool
	Ask               bool
	SendingText       bool
	NoCompress        bool
	IP                string
	Overwrite         bool
	Rename            bool
	Curve             string
	HashAlgorithm     string
	ThrottleUpload    string
	ZipFolder         bool
	TestFlag          bool
	GitIgnore         bool
	MulticastAddress  string
	ShowQrCode        bool
	Exclude           []string
	ExcludeFile       []string
	Quiet             bool
	DisableClipboard  bool
	ExtendedClipboard bool
}

type SimpleMessage struct {
	Bytes   []byte
	Bytes2  []byte
	Kind    string
	Version int
	Curve   string
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
	CurrentFileChunkCount  int
	CurrentFileIsClosed    bool
	LastFolder             string

	TotalSent              int64
	TotalChunksTransferred int
	limiter                *rate.Limiter
	dataAEAD               cipher.AEAD

	// tcp connections
	conn                    []*comm.Comm
	baseRoomName            string
	pakePassphrase          string
	pakeInitiator           []byte
	pakeResponder           []byte
	pakeCurve               string
	pakeKeys                pakekey.Keys
	pakeConfirmationPending bool
	nextReconnectRoom       string
	relayControlAddress     string
	reconnectRelayAddresses []string
	reconnectRelayMu        sync.Mutex
	reconnectVersion        int
	peerReconnectVersion    int
	peerPerFileCompression  bool
	senderRouteReady        chan struct{}
	filesReady              chan struct{}
	filesReadyErr           error
	senderRouteReadyOnce    sync.Once
	transferStarted         atomic.Bool
	// localRelayPort is the control port of the ephemeral local relay started by
	// setupLocalRelay(). It is captured before any goroutines that might
	// overwrite c.Options.RelayPorts are launched.
	localRelayPort string

	bar             *progressbar.ProgressBar
	longestFilename int
	firstSend       bool

	mutex                    *sync.Mutex
	receiveMutex             *sync.Mutex
	fread                    *os.File
	numfinished              int
	quit                     chan bool
	finishedNum              int
	numberOfTransferredFiles int

	// ctx.go for graceful shutdown
	*stop
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
	IsCompressed bool        `json:"c"`
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
	ReconnectVersion          int
	Features                  []string `json:",omitempty"`
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
	ReconnectVersion       int
	NextReconnectRoom      string
	Features               []string `json:",omitempty"`
}

const perFileCompressionFeature = "per-file-compression-v1"

// ErrRelayConnection marks a failure to establish a relay control or data
// connection. Callers may use it to invalidate cached relay selections without
// treating peer or transfer failures as relay availability failures.
var ErrRelayConnection = errors.New("relay connection failed")

func supportsFeature(features []string, wanted string) bool {
	for _, feature := range features {
		if feature == wanted {
			return true
		}
	}
	return false
}

// New establishes a new connection for transferring files between two instances.
func New(ops Options) (c *Client, err error) {
	c = new(Client)
	c.FilesHasFinished = make(map[int]struct{})

	// setup basic info
	c.Options = ops
	Debug(c.Options.Debug)

	// redirect stderr to null if quiet mode is enabled
	if c.Options.Quiet {
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err == nil {
			os.Stderr = devNull
		}
	}

	codeComponents, err := codephrase.Parse(c.Options.SharedSecret)
	if err != nil {
		return
	}
	c.Options.RoomName = codeComponents.RoomName
	c.pakePassphrase = codeComponents.PAKEPassphrase
	c.baseRoomName = c.Options.RoomName
	c.reconnectVersion = ReconnectVersion

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
		c.Pake, err = pakekey.Init(
			[]byte(c.pakePassphrase),
			0,
			c.Options.Curve,
			pakekey.PurposeTransfer,
			c.Options.RoomName,
		)
	}
	if err != nil {
		return
	}

	c.mutex = &sync.Mutex{}
	c.receiveMutex = &sync.Mutex{}
	c.senderRouteReady = make(chan struct{})
	c.stop = newStop(context.Background())
	return
}

type transferDisconnectError struct {
	err error
}

func (e transferDisconnectError) Error() string {
	return fmt.Sprintf("transfer disconnected: %v", e.err)
}

func (e transferDisconnectError) Unwrap() error {
	return e.err
}

type pakeHandshakeError struct {
	err error
}

type incompatiblePakeVersionError struct {
	got int
}

func (e incompatiblePakeVersionError) Error() string {
	return fmt.Sprintf(
		"peer uses unsupported PAKE protocol version %d; upgrade both croc clients",
		e.got,
	)
}

func (e pakeHandshakeError) Error() string {
	return fmt.Sprintf("pake not successful: %v", e.err)
}

func (e pakeHandshakeError) Unwrap() error {
	return e.err
}

type transferAttemptState struct {
	errc    chan error
	control *comm.Comm
	once    sync.Once

	sendMu    sync.Mutex
	sendDone  int
	sendClose sync.Once
}

func (a *transferAttemptState) report(err error) {
	if err == nil {
		return
	}
	a.once.Do(func() {
		select {
		case a.errc <- err:
		default:
		}
		if a.control != nil {
			a.control.Close()
		}
	})
}

func (a *transferAttemptState) finishSenderData(total int, file *os.File) {
	if file == nil {
		return
	}
	a.sendMu.Lock()
	a.sendDone++
	done := a.sendDone == total
	a.sendMu.Unlock()
	if done {
		a.sendClose.Do(func() {
			log.Debug("closing file")
			if err := file.Close(); err != nil {
				if !errors.Is(err, os.ErrClosed) {
					log.Errorf("error closing file: %v", err)
				}
			}
		})
	}
}

func generateReconnectRoom() (string, error) {
	room := make([]byte, 32)
	if _, err := rand.Read(room); err != nil {
		return "", err
	}
	return hex.EncodeToString(room), nil
}

func reconnectBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	delay := 100 * time.Millisecond
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= 5*time.Second {
			return 5 * time.Second
		}
	}
	return delay
}

func isTransferDisconnectError(err error) bool {
	var disconnect transferDisconnectError
	return err != nil && errors.As(err, &disconnect)
}

func normalizeRelayAddress(address string) string {
	if address == "" {
		return ""
	}
	host, port, _ := net.SplitHostPort(address)
	if port == "" {
		host = address
		port = models.DEFAULT_PORT
	}
	return net.JoinHostPort(host, port)
}

func (c *Client) rememberReconnectRelayAddress(address string) {
	address = normalizeRelayAddress(address)
	if address == "" {
		return
	}
	c.reconnectRelayMu.Lock()
	defer c.reconnectRelayMu.Unlock()
	for _, existing := range c.reconnectRelayAddresses {
		if existing == address {
			return
		}
	}
	c.reconnectRelayAddresses = append(c.reconnectRelayAddresses, address)
}

func (c *Client) setRelayControlAddress(address string) {
	address = normalizeRelayAddress(address)
	if address == "" {
		return
	}
	c.reconnectRelayMu.Lock()
	defer c.reconnectRelayMu.Unlock()
	c.relayControlAddress = address
	c.Options.RelayAddress = address
	reconnectRelayAddresses := []string{address}
	for _, existing := range c.reconnectRelayAddresses {
		if existing != address {
			reconnectRelayAddresses = append(reconnectRelayAddresses, existing)
		}
	}
	c.reconnectRelayAddresses = reconnectRelayAddresses
}

func (c *Client) reconnectRelayCandidates() []string {
	c.reconnectRelayMu.Lock()
	defer c.reconnectRelayMu.Unlock()
	var candidates []string
	if c.relayControlAddress != "" {
		candidates = append(candidates, c.relayControlAddress)
	}
	for _, address := range c.reconnectRelayAddresses {
		seen := false
		for _, candidate := range candidates {
			if candidate == address {
				seen = true
				break
			}
		}
		if !seen {
			candidates = append(candidates, address)
		}
	}
	return candidates
}

func (c *Client) currentRelayControlAddress() string {
	c.reconnectRelayMu.Lock()
	defer c.reconnectRelayMu.Unlock()
	return c.relayControlAddress
}

func (c *Client) activeTransferStarted() bool {
	return c.transferStarted.Load()
}

func (c *Client) markTransferStarted() {
	c.transferStarted.Store(true)
}

func (c *Client) markSenderRouteReady() {
	c.senderRouteReadyOnce.Do(func() {
		close(c.senderRouteReady)
	})
}

func (c *Client) canRetryTransfer(err error, attempt int) bool {
	if err == nil || c.SuccessfulTransfer {
		return false
	}
	if attempt >= maxReconnectAttempts {
		return false
	}
	if c.peerReconnectVersion < ReconnectVersion || c.reconnectVersion < ReconnectVersion {
		return false
	}
	if c.nextReconnectRoom == "" {
		return false
	}
	if ctxErr := c.ctxErr(); ctxErr != nil {
		return false
	}
	return isTransferDisconnectError(err)
}

func (c *Client) closeAttempt() {
	for _, conn := range c.conn {
		if conn != nil {
			conn.Close()
		}
	}
	c.receiveMutex.Lock()
	if c.CurrentFile != nil && !c.CurrentFileIsClosed {
		if err := c.CurrentFile.Close(); err != nil {
			log.Tracef("closing current receive file: %v", err)
		}
		c.CurrentFileIsClosed = true
	}
	c.receiveMutex.Unlock()
	if c.fread != nil {
		if err := c.fread.Close(); err != nil {
			log.Tracef("closing current send file: %v", err)
		}
		c.fread = nil
	}
}

func (c *Client) resetForReconnectAttempt(attempt int) error {
	log.Debugf("resetting transfer state for reconnect attempt %d", attempt)
	if c.nextReconnectRoom == "" {
		return transferDisconnectError{err: fmt.Errorf("missing reconnect room")}
	}
	c.Options.RoomName = c.nextReconnectRoom
	c.Step1ChannelSecured = false
	c.Step2FileInfoTransferred = false
	c.Step3RecipientRequestFile = false
	c.Step4FileTransferred = false
	c.SuccessfulTransfer = false
	c.Key = nil
	c.dataAEAD = nil
	c.pakeInitiator = nil
	c.pakeResponder = nil
	c.pakeCurve = ""
	c.pakeKeys = pakekey.Keys{}
	c.pakeConfirmationPending = false
	c.peerPerFileCompression = false
	c.CurrentFileChunkRanges = nil
	c.CurrentFileChunkCount = 0
	c.TotalSent = 0
	c.TotalChunksTransferred = 0
	c.numfinished = 0
	if !c.Options.IsSender {
		pakeInstance, err := pakekey.Init(
			[]byte(c.pakePassphrase),
			0,
			c.Options.Curve,
			pakekey.PurposeTransfer,
			c.Options.RoomName,
		)
		if err != nil {
			return err
		}
		c.Pake = pakeInstance
	}
	return nil
}

func (c *Client) transferWithReconnect(connectAttempt func(attempt int) error) error {
	var lastErr error
	var lastDisconnectErr error
	for attempt := 0; attempt <= maxReconnectAttempts; attempt++ {
		if attempt > 0 {
			delay := reconnectBackoff(attempt)
			log.Debugf("reconnect attempt %d after %s", attempt, delay)
			time.Sleep(delay)
			if err := c.resetForReconnectAttempt(attempt); err != nil {
				return err
			}
		}
		if err := connectAttempt(attempt); err != nil {
			if attempt > 0 && lastDisconnectErr != nil {
				return fmt.Errorf("%w (reconnect attempt %d failed: %v)", lastDisconnectErr, attempt, err)
			}
			lastErr = err
		} else {
			lastErr = c.transfer()
		}
		if lastErr == nil {
			return nil
		}
		log.Debugf("transfer attempt %d failed: %v", attempt, lastErr)
		if attempt >= maxReconnectAttempts && isTransferDisconnectError(lastErr) {
			return fmt.Errorf("transfer disconnected after %d reconnect attempts: %w", maxReconnectAttempts, lastErr)
		}
		if !c.canRetryTransfer(lastErr, attempt) {
			return lastErr
		}
		role := "Receiver"
		if c.Options.IsSender {
			role = "Sender"
		}
		output, colorEnabled := termui.Output(os.Stderr)
		fmt.Fprintf(output, "\n%s detected a transfer interruption. %s\n", role, termui.Warning("Retrying securely...", colorEnabled))
		lastDisconnectErr = lastErr
		c.closeAttempt()
	}
	return fmt.Errorf("transfer disconnected after %d reconnect attempts: %w", maxReconnectAttempts, lastErr)
}

// TransferOptions for sending
type TransferOptions struct {
	PathToFiles      []string
	KeepPathInRemote bool
}

// helper function checking for an empty folder
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

func isLocalReceivePath(name string) bool {
	name = path.Clean(strings.ReplaceAll(name, "\\", "/"))
	return name == "." || (filepath.IsLocal(filepath.FromSlash(name)) && !path.IsAbs(name))
}

func normalizeReceiveFolder(folder string) (string, error) {
	cleanFolder := path.Clean(strings.ReplaceAll(folder, "\\", "/"))
	if cleanFolder == "" {
		cleanFolder = "."
	}
	if !isLocalReceivePath(cleanFolder) {
		return "", fmt.Errorf("filename must be a local path: '%s'", folder)
	}
	if strings.Contains(cleanFolder, ".ssh") {
		return "", fmt.Errorf("invalid path detected: '%s'", folder)
	}
	if err := utils.ValidFileName(cleanFolder); err != nil {
		return "", err
	}
	return cleanFolder, nil
}

func normalizeReceiveFilePath(folder, name string) (string, string, error) {
	cleanFolder, err := normalizeReceiveFolder(folder)
	if err != nil {
		return "", "", err
	}
	cleanName := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if cleanName == "." || cleanName == "" || cleanName != path.Base(cleanName) || !isLocalReceivePath(cleanName) {
		return "", "", fmt.Errorf("filename must be a local path: '%s'", name)
	}
	if err := utils.ValidFileName(cleanName); err != nil {
		return "", "", err
	}
	destination := path.Clean(path.Join(cleanFolder, cleanName))
	if !isLocalReceivePath(destination) {
		return "", "", fmt.Errorf("filename must be a local path: '%s'", path.Join(folder, name))
	}
	if err := utils.ValidFileName(destination); err != nil {
		return "", "", err
	}
	return cleanFolder, destination, nil
}

func validateReceiveSymlinkTarget(folder, target string) error {
	cleanTarget := path.Clean(strings.ReplaceAll(target, "\\", "/"))
	if cleanTarget == "." || cleanTarget == "" || path.IsAbs(cleanTarget) || filepath.IsAbs(filepath.FromSlash(cleanTarget)) {
		return fmt.Errorf("symlink target must be a local path: '%s'", target)
	}
	resolvedTarget := path.Clean(path.Join(folder, cleanTarget))
	if !isLocalReceivePath(resolvedTarget) {
		return fmt.Errorf("symlink target escapes receive directory: '%s'", target)
	}
	return nil
}

func validateReceiveMetadata(files []FileInfo, emptyFolders []FileInfo) ([]FileInfo, []FileInfo, error) {
	normalizedFiles := make([]FileInfo, len(files))
	normalizedEmptyFolders := make([]FileInfo, len(emptyFolders))
	destinations := make(map[string]struct{}, len(files)+len(emptyFolders))

	for i, fi := range files {
		cleanFolder, destination, err := normalizeReceiveFilePath(fi.FolderRemote, fi.Name)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := destinations[destination]; ok {
			return nil, nil, fmt.Errorf("duplicate destination path: '%s'", destination)
		}
		destinations[destination] = struct{}{}
		if fi.Symlink != "" {
			if err := validateReceiveSymlinkTarget(cleanFolder, fi.Symlink); err != nil {
				return nil, nil, err
			}
		}
		normalizedFiles[i] = fi
		normalizedFiles[i].FolderRemote = cleanFolder
		normalizedFiles[i].Name = path.Base(strings.ReplaceAll(fi.Name, "\\", "/"))
	}

	for i, fi := range emptyFolders {
		cleanFolder, err := normalizeReceiveFolder(fi.FolderRemote)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := destinations[cleanFolder]; ok {
			return nil, nil, fmt.Errorf("duplicate destination path: '%s'", cleanFolder)
		}
		destinations[cleanFolder] = struct{}{}
		normalizedEmptyFolders[i] = fi
		normalizedEmptyFolders[i].FolderRemote = cleanFolder
	}

	return normalizedFiles, normalizedEmptyFolders, nil
}

func rejectSymlinkDestination(pathToFile string) error {
	return utils.RejectSymlinkPath(".", pathToFile)
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
	// Only "." or a component that is exactly ".." (optionally followed by more
	// path segments) means childPath is not under parentPath. A bare HasPrefix
	// on ".." wrongly rejects legitimate children whose name merely starts with
	// ".." (e.g. "..cache"), matching the pathWithin helper in utils.
	return relPath != ".." && !strings.HasPrefix(relPath, ".."+string(os.PathSeparator))
}

// This function retrieves the important file information
// for every file that will be transferred
func GetFilesInfo(fnames []string, zipfolder bool, ignoreGit bool, exclusions []string) (filesInfo []FileInfo, emptyFolders []FileInfo, totalNumberFolders int, err error) {
	return GetFilesInfoWithExactExclusions(fnames, zipfolder, ignoreGit, exclusions, nil)
}

// GetFilesInfoWithExactExclusions retrieves file information while applying
// both the legacy substring exclusions and exact relative-path exclusions.
func GetFilesInfoWithExactExclusions(fnames []string, zipfolder bool, ignoreGit bool, exclusions, exactExclusions []string) (filesInfo []FileInfo, emptyFolders []FileInfo, totalNumberFolders int, err error) {
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
	ignoredPaths := make(map[string]bool)
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
			err = utils.ZipDirectoryWithExactExclusions(dest, fpath, ignoredPaths, exclusions, exactExclusions)
			if err != nil {
				return
			}
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
					relPath, relErr := filepath.Rel(absPath, pathName)
					if relErr == nil && exactPathExcluded(exactExclusions, relPath) {
						if info.IsDir() {
							return filepath.SkipDir
						}
						return nil
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
			if exactPathExcluded(exactExclusions, stat.Name()) {
				continue
			}
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

func exactPathExcluded(exclusions []string, candidate string) bool {
	candidate = utils.NormalizeRelativePath(candidate)
	for _, exclusion := range exclusions {
		if candidate == utils.NormalizeRelativePath(exclusion) {
			return true
		}
	}
	return false
}

func (c *Client) sendCollectFiles(filesInfo []FileInfo) (err error) {
	c.FilesToTransfer = filesInfo
	totalFilesSize := int64(0)
	compressionSample := make([]byte, compressionSampleSize)
	var compressionOutput []byte

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
		if !c.Options.NoCompress && fileInfo.Mode.IsRegular() && fileInfo.Size > 0 {
			c.FilesToTransfer[i].IsCompressed, compressionOutput = shouldCompressFile(
				fullPath,
				compressionSample,
				compressionOutput,
			)
		}

		c.FilesToTransfer[i].Hash, err = c.stop.hash(fullPath, c.Options.HashAlgorithm, fileInfo.Size > 1e7)
		log.Debugf("hashed %s to %x using %s", fullPath, c.FilesToTransfer[i].Hash, c.Options.HashAlgorithm)
		totalFilesSize += fileInfo.Size
		if err != nil {
			return
		}
		log.Debugf("file %d info: %+v", i, c.FilesToTransfer[i])
		fmt.Fprintf(os.Stderr, "\r                                 ")
		output, _ := termui.Output(os.Stderr)
		fmt.Fprintf(output, "\rSending %d files (%s)", i, utils.ByteCountDecimal(totalFilesSize))
	}
	log.Debugf("longestFilename: %+v", c.longestFilename)
	fname := fmt.Sprintf("%d files", len(c.FilesToTransfer))
	folderName := fmt.Sprintf("%d folders", c.TotalNumberFolders)
	displayName := ""
	if len(c.FilesToTransfer) == 1 {
		displayName = c.FilesToTransfer[0].Name
		fname = quotedFilename(displayName, false)
	}
	if strings.HasPrefix(displayName, "croc-stdin-") {
		displayName = "stdin"
		fname = quotedFilename(displayName, false)
		if c.Options.SendingText {
			displayName = "text"
			fname = quotedFilename(displayName, false)
		}
	}

	fmt.Fprintf(os.Stderr, "\r                                 ")
	output, colorEnabled := termui.Output(os.Stderr)
	if displayName != "" {
		fname = quotedFilename(displayName, colorEnabled)
	}
	if c.TotalNumberFolders > 0 {
		fmt.Fprintf(output, "\rSending %s and %s (%s)\n", fname, folderName, utils.ByteCountDecimal(totalFilesSize))
	} else {
		fmt.Fprintf(output, "\rSending %s (%s)\n", fname, utils.ByteCountDecimal(totalFilesSize))
	}
	return
}

const compressionSampleSize = 256 << 10

// shouldCompressFile samples the beginning of a file. Deflate output within
// two percent of the input is treated as incompressible, avoiding codec work
// and slight wire expansion for archives, media, and encrypted files.
func shouldCompressFile(path string, sample, compressedOutput []byte) (bool, []byte) {
	file, err := os.Open(path)
	if err != nil {
		return true, compressedOutput
	}
	defer file.Close()
	n, err := file.Read(sample)
	if err != nil && err != io.EOF {
		return true, compressedOutput
	}
	if n == 0 {
		return false, compressedOutput
	}
	compressedOutput = compress.CompressTo(compressedOutput, sample[:n])
	return len(compressedOutput)*100 < n*98, compressedOutput
}

func (c *Client) currentFileUsesCompression() bool {
	if c.Options.NoCompress {
		return false
	}
	// Older peers only understand the global switch and expect all chunks to
	// be compressed whenever NoCompress is false.
	if !c.peerPerFileCompression {
		return true
	}
	return c.FilesToTransferCurrentNum >= 0 &&
		c.FilesToTransferCurrentNum < len(c.FilesToTransfer) &&
		c.FilesToTransfer[c.FilesToTransferCurrentNum].IsCompressed
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
	// Capture the local relay control port before any goroutine that handles
	// the external relay can overwrite c.Options.RelayPorts.
	c.localRelayPort = c.Options.RelayPorts[0]
	localRelayPorts := append([]string(nil), c.Options.RelayPorts...)
	localRelayBanner := strings.Join(localRelayPorts[1:], ",")
	for _, port := range localRelayPorts {
		go func(portStr string) {
			debugString := "warn"
			if c.Options.Debug {
				debugString = "debug"
			}
			err := c.stop.run(
				debugString,
				"",
				portStr,
				c.Options.RelayPassword,
				localRelayBanner)
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
		Payload:   []byte("croc" + c.localRelayPort),
		Delay:     20 * time.Millisecond,
		TimeLimit: timeLimit,
		StopChan:  c.stop.stopChan,
	}
	if useipv6 {
		settings.IPVersion = peerdiscovery.IPv6
	} else {
		settings.MulticastAddress = c.Options.MulticastAddress
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
	if !c.Options.OnlyLocal {
		c.rememberReconnectRelayAddress(c.Options.RelayAddress)
		c.rememberReconnectRelayAddress(c.Options.RelayAddress6)
	}
	localControlAddress := "127.0.0.1:" + c.localRelayPort
	var banner string
	conn, banner, ipaddr, err := tcp.ConnectToTCPServer(localControlAddress, c.Options.RelayPassword, c.Options.RoomName)
	log.Debugf("banner: %s", banner)
	if err != nil {
		err = fmt.Errorf("could not connect to 127.0.0.1:%s: %w", c.localRelayPort, err)
		log.Debug(err)
		// not really an error because it will try to connect over the actual relay
		return
	}
	log.Debugf("local connection established: %+v", conn)
	for {
		if err := c.ctxErr(); err != nil {
			errchan <- err
			return
		}
		data, _ := conn.Receive()
		if bytes.Equal(data, handshakeRequest) {
			break
		} else if bytes.Equal(data, []byte{1}) {
			log.Trace("got ping")
		} else {
			log.Debugf("instead of handshake got: %s", data)
		}
	}
	c.setRelayControlAddress(localControlAddress)
	c.conn[0] = conn
	log.Debug("exchanged header message")
	c.Options.RelayPorts = strings.Split(banner, ",")
	if c.Options.NoMultiplexing {
		log.Debug("no multiplexing")
		c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
	}
	c.ExternalIP = ipaddr
	c.markSenderRouteReady()
	errchan <- c.transferWithReconnect(func(attempt int) error {
		if attempt == 0 {
			return nil
		}
		return c.senderReconnectRelayAttempt(attempt)
	})
}

func (c *Client) senderWaitForHandshake(conn *comm.Comm) error {
	var kB []byte
	for {
		if err := c.ctxErr(); err != nil {
			return err
		}
		var dataMessage SimpleMessage
		log.Trace("waiting for bytes")
		data, errConn := conn.Receive()
		if errConn != nil {
			log.Tracef("[%+v] had error: %s", conn, errConn.Error())
			return errConn
		}
		json.Unmarshal(data, &dataMessage)
		log.Tracef("data: %+v '%s'", data, data)
		log.Tracef("dataMessage: %+v", dataMessage)
		if kB != nil {
			var decryptErr error
			var dataDecrypt []byte
			dataDecrypt, decryptErr = crypt.Decrypt(data, kB)
			if decryptErr != nil {
				log.Tracef("error decrypting: %v: '%s'", decryptErr, data)
				if strings.Contains(decryptErr.Error(), "message authentication failed") {
					return decryptErr
				}
			} else {
				data = dataDecrypt
				log.Tracef("decrypted: %s", data)
			}
		}
		if bytes.Equal(data, ipRequest) {
			log.Tracef("got ipRequest")
			var ips []string
			if !c.Options.DisableLocal {
				var err error
				ips, err = utils.GetLocalIPs()
				if err != nil {
					log.Tracef("error getting local ips: %v", err)
				}
				ips = append([]string{c.localRelayPort}, ips...)
			}
			log.Tracef("sending ips: %+v", ips)
			bips, err := json.Marshal(ips)
			if err != nil {
				log.Tracef("error marshalling ips: %v", err)
			}
			bips, err = crypt.Encrypt(bips, kB)
			if err != nil {
				log.Tracef("error encrypting ips: %v", err)
			}
			if err = conn.Send(bips); err != nil {
				return err
			}
		} else if dataMessage.Kind == "pake1" {
			log.Trace("got pake1")
			if dataMessage.Version != pakekey.ProtocolVersion {
				return incompatiblePakeVersionError{got: dataMessage.Version}
			}
			if dataMessage.Curve == "" {
				return pakeHandshakeError{err: fmt.Errorf("local probe did not specify a curve")}
			}
			B, pakeError := pakekey.Init(
				[]byte(c.pakePassphrase),
				1,
				dataMessage.Curve,
				pakekey.PurposeLocalProbe,
				c.Options.RoomName,
			)
			initiator := append([]byte(nil), dataMessage.Bytes...)
			if pakeError == nil {
				pakeError = B.Update(initiator)
			}
			if pakeError == nil {
				var sharedKey []byte
				sharedKey, pakeError = B.SessionKey()
				if pakeError == nil {
					responder := B.Bytes()
					salt := make([]byte, pakekey.SaltSize)
					if _, pakeError = rand.Read(salt); pakeError != nil {
						return pakeError
					}
					var keys pakekey.Keys
					keys, pakeError = pakekey.Derive(sharedKey, pakekey.Context{
						Purpose:   pakekey.PurposeLocalProbe,
						Room:      c.Options.RoomName,
						Curve:     dataMessage.Curve,
						Initiator: initiator,
						Responder: responder,
						Salt:      salt,
					})
					if pakeError != nil {
						return pakeError
					}
					kB = keys.EncryptionKey
					dataMessage.Bytes = responder
					dataMessage.Bytes2 = salt
					dataMessage.Kind = "pake2"
					dataMessage.Version = pakekey.ProtocolVersion
					data, _ = json.Marshal(dataMessage)
					if pakeError = conn.Send(data); pakeError != nil {
						return pakeError
					}
				}
			}
			if pakeError != nil {
				return pakeError
			}
		} else if bytes.Equal(data, handshakeRequest) {
			log.Trace("got handshake")
			return nil
		} else if bytes.Equal(data, []byte{1}) {
			log.Trace("got ping")
			continue
		} else {
			log.Tracef("[%+v] got weird bytes: %+v", conn, data)
			return fmt.Errorf("gracefully refusing using the public relay")
		}
	}
}

func isFatalSenderRouteError(err error) bool {
	if err == nil {
		return false
	}
	errString := err.Error()
	return strings.Contains(errString, "refusing files") ||
		strings.Contains(errString, "bad password") ||
		strings.Contains(errString, "message authentication failed")
}

func (c *Client) waitForAlternateSenderRoute(errchan <-chan error, originalErr error) error {
	timeout := time.NewTimer(alternateSenderRouteTimeout)
	defer timeout.Stop()

	select {
	case <-c.senderRouteReady:
		select {
		case err := <-errchan:
			return err
		case <-c.stop.ctx.Done():
			return c.stop.ctx.Err()
		}
	case err := <-errchan:
		return err
	case <-timeout.C:
		log.Debug("timed out waiting for alternate sender route")
		return originalErr
	case <-c.stop.ctx.Done():
		return c.stop.ctx.Err()
	}
}

func (c *Client) reconnectRelayAttempt(handshake func(*comm.Comm) error) error {
	room := c.nextReconnectRoom
	if room == "" {
		return transferDisconnectError{err: fmt.Errorf("missing reconnect room")}
	}
	candidates := c.reconnectRelayCandidates()
	if len(candidates) == 0 {
		return fmt.Errorf("missing relay control address")
	}
	var reconnectErrors []string
	for _, address := range candidates {
		conn, banner, ipaddr, err := tcp.ConnectToTCPServer(address, c.Options.RelayPassword, room)
		if err != nil {
			reconnectErrors = append(reconnectErrors, fmt.Sprintf("%s: %v", address, err))
			continue
		}
		errc := make(chan error, 1)
		go func() {
			errc <- handshake(conn)
		}()
		select {
		case err = <-errc:
		case <-time.After(reconnectCandidateHandshakeTimeout):
			err = fmt.Errorf("timed out waiting for reconnect handshake")
		case <-c.stop.ctx.Done():
			err = c.stop.ctx.Err()
		}
		if err != nil {
			conn.Close()
			reconnectErrors = append(reconnectErrors, fmt.Sprintf("%s: %v", address, err))
			continue
		}
		c.setRelayControlAddress(address)
		c.conn[0] = conn
		c.Options.RoomName = room
		c.Options.RelayPorts = strings.Split(banner, ",")
		if c.Options.NoMultiplexing {
			c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
		}
		c.ExternalIP = ipaddr
		return nil
	}
	return fmt.Errorf("could not reconnect to any relay: %s", strings.Join(reconnectErrors, "; "))
}

func (c *Client) senderReconnectRelayAttempt(attempt int) error {
	return c.reconnectRelayAttempt(c.senderWaitForHandshake)
}

func (c *Client) receiverReconnectRelayAttempt(attempt int) error {
	if err := c.reconnectRelayAttempt(func(conn *comm.Comm) error {
		return conn.Send(handshakeRequest)
	}); err != nil {
		return err
	}
	log.Debug("exchanged reconnect header message")
	return nil
}

// Send will send the specified file
func (c *Client) Send(filesInfo []FileInfo, emptyFoldersToTransfer []FileInfo, totalNumberFolders int) (err error) {
	go c.stop.done()
	defer c.stop.Cancel()
	c.EmptyFoldersToTransfer = emptyFoldersToTransfer
	c.TotalNumberFolders = totalNumberFolders
	c.TotalNumberOfContents = len(filesInfo)
	c.FilesToTransfer = filesInfo
	c.filesReady = make(chan struct{})
	hashResult := make(chan error, 1)
	go func() {
		c.filesReadyErr = c.sendCollectFiles(filesInfo)
		close(c.filesReady)
		hashResult <- c.filesReadyErr
	}()
	flags := &strings.Builder{}
	if !c.Options.PublicRelay && c.Options.RelayAddress != models.DEFAULT_RELAY && !c.Options.OnlyLocal {
		flags.WriteString("--relay " + c.Options.RelayAddress + " ")
	}
	if c.Options.RelayPassword != models.DEFAULT_PASSPHRASE {
		flags.WriteString("--pass " + c.Options.RelayPassword + " ")
	}
	webURL := webReceiveURL(c.Options.SharedSecret)
	output, colorEnabled := termui.Output(os.Stderr)
	fmt.Fprint(output, formatSendInstructions(c.Options.SharedSecret, flags.String(), webURL, colorEnabled))
	if !c.Options.DisableClipboard {
		clipboardText := formatClipboardText(c.Options.SharedSecret, flags.String(), c.Options.ExtendedClipboard)
		copyToClipboard(clipboardText, c.Options.Quiet, c.Options.ExtendedClipboard)
	}
	if c.Options.ShowQrCode {
		showReceiveCommandQrCode(webURL)
	}
	if c.Options.Ask {
		machid, _ := machineid.ID()
		fmt.Fprintf(os.Stderr, "\rYour machine ID is '%s'\n", machid)
	}
	// c.spinner.Suffix = " waiting for recipient..."
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
			var selectedAddress string
			var routeErr error
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
				conn, banner, ipaddr, routeErr = tcp.ConnectToTCPServer(address, c.Options.RelayPassword, c.Options.RoomName, durations[i])
				if routeErr == nil {
					selectedAddress = address
					break
				}
				log.Debugf("could not establish '%s'", address)
			}
			if conn == nil && routeErr == nil {
				routeErr = fmt.Errorf("could not connect")
			}
			if routeErr != nil {
				routeErr = fmt.Errorf("%w: could not connect to %s: %v", ErrRelayConnection, c.Options.RelayAddress, routeErr)
				log.Debug(routeErr)
				errchan <- routeErr
				return
			}
			log.Debugf("banner: %s", banner)
			log.Debugf("connection established: %+v", conn)
			if routeErr = c.senderWaitForHandshake(conn); routeErr != nil {
				errchan <- routeErr
				return
			}

			c.setRelayControlAddress(selectedAddress)
			c.conn[0] = conn
			c.Options.RelayPorts = strings.Split(banner, ",")
			if c.Options.NoMultiplexing {
				log.Debug("no multiplexing")
				c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
			}
			c.ExternalIP = ipaddr
			log.Debug("exchanged header message")
			c.markSenderRouteReady()
			errchan <- c.transferWithReconnect(func(attempt int) error {
				if attempt == 0 {
					return nil
				}
				return c.senderReconnectRelayAttempt(attempt)
			})
		}()
	}

	select {
	case err = <-hashResult:
		if err != nil {
			return err
		}
		err = <-errchan
	case err = <-errchan:
	}
	if err == nil {
		return // no error
	} else {
		log.Debugf("error from errchan: %v", err)
		if strings.Contains(err.Error(), "could not secure channel") {
			return err
		}
	}
	if !c.Options.DisableLocal {
		if isFatalSenderRouteError(err) {
			return err
		}
		log.Debugf("waiting for alternate sender route after: %v", err)
		err = c.waitForAlternateSenderRoute(errchan, err)
	}
	return err
}

func showReceiveCommandQrCode(command string) {
	qrCode, err := qrcode.New(command, qrcode.Medium)
	if err == nil {
		fmt.Println(qrCode.ToSmallString(false))
	}
}

// ShowReceiveCommandQrCode prints a QR code for a share URL.
func ShowReceiveCommandQrCode(command string) {
	showReceiveCommandQrCode(command)
}

func webReceiveURL(code string) string {
	return "https://getcroc.com/?code=" + url.QueryEscape(code)
}

type peerDiscoveryResult struct {
	discoveries []peerdiscovery.Discovered
	err         error
}

var (
	receivePeerDiscoveryTimeout   = 500 * time.Millisecond
	receivePeerDiscoveryTimeLimit = 200 * time.Millisecond
	peerDiscover                  = peerdiscovery.Discover
)

func (c *Client) discoverReceivePeers() (discoveries []peerdiscovery.Discovered) {
	resultChan := make(chan peerDiscoveryResult, 2)
	stopDiscovery := make(chan struct{})
	var closeOnce sync.Once
	closeDiscovery := func() {
		closeOnce.Do(func() {
			close(stopDiscovery)
		})
	}
	defer closeDiscovery()
	go func() {
		select {
		case <-c.stop.stopChan:
			closeDiscovery()
		case <-stopDiscovery:
		}
	}()

	startDiscovery := func(settings peerdiscovery.Settings) {
		settings.StopChan = stopDiscovery
		discover := peerDiscover
		go func() {
			found, err := discover(settings)
			resultChan <- peerDiscoveryResult{
				discoveries: found,
				err:         err,
			}
		}()
	}

	startDiscovery(peerdiscovery.Settings{
		Limit:            1,
		Payload:          []byte("ok"),
		Delay:            20 * time.Millisecond,
		TimeLimit:        receivePeerDiscoveryTimeLimit,
		MulticastAddress: c.Options.MulticastAddress,
	})
	startDiscovery(peerdiscovery.Settings{
		Limit:     1,
		Payload:   []byte("ok"),
		Delay:     20 * time.Millisecond,
		TimeLimit: receivePeerDiscoveryTimeLimit,
		IPVersion: peerdiscovery.IPv6,
	})

	timer := time.NewTimer(receivePeerDiscoveryTimeout)
	defer timer.Stop()
	for remaining := 2; remaining > 0; remaining-- {
		select {
		case result := <-resultChan:
			if result.err != nil {
				log.Debugf("peer discovery failed: %v", result.err)
				continue
			}
			discoveries = append(discoveries, result.discoveries...)
		case <-timer.C:
			log.Debug("peer discovery timed out")
			return
		case <-c.stop.stopChan:
			return
		}
	}
	return
}

// Receive will receive a file
func (c *Client) Receive() (err error) {
	go c.stop.done()
	defer c.stop.Cancel()
	output, _ := termui.Output(os.Stderr)
	fmt.Fprint(output, "connecting...")
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
		discoveries := c.discoverReceivePeers()

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
			c.setRelayControlAddress(address)
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
			A, err = pakekey.Init(
				[]byte(c.pakePassphrase),
				0,
				c.Options.Curve,
				pakekey.PurposeLocalProbe,
				c.Options.RoomName,
			)
			if err != nil {
				return err
			}
			initiator := A.Bytes()
			dataMessage := SimpleMessage{
				Bytes:   initiator,
				Kind:    "pake1",
				Version: pakekey.ProtocolVersion,
				Curve:   c.Options.Curve,
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
			if dataMessage.Version != pakekey.ProtocolVersion {
				return incompatiblePakeVersionError{got: dataMessage.Version}
			}
			if dataMessage.Curve != c.Options.Curve {
				return fmt.Errorf("local PAKE curve changed from %s to %s", c.Options.Curve, dataMessage.Curve)
			}
			if len(dataMessage.Bytes2) != pakekey.SaltSize {
				return fmt.Errorf("invalid local PAKE salt length %d", len(dataMessage.Bytes2))
			}
			err = A.Update(dataMessage.Bytes)
			if err != nil {
				return
			}
			var sharedKey []byte
			sharedKey, err = A.SessionKey()
			if err != nil {
				return
			}
			keys, err := pakekey.Derive(sharedKey, pakekey.Context{
				Purpose:   pakekey.PurposeLocalProbe,
				Room:      c.Options.RoomName,
				Curve:     c.Options.Curve,
				Initiator: initiator,
				Responder: dataMessage.Bytes,
				Salt:      dataMessage.Bytes2,
			})
			if err != nil {
				return err
			}
			kA := keys.EncryptionKey

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

				// For peer-to-peer connectivity within a LAN, the sender and receiver don't need to be on the same subnet.
				// Even with NAT routers in their respective local networks,
				// a receiver behind NAT can establish direct access to the sender without requiring internet connectivity.
				// Conversely, the local networks on the sender and receiver may overlap but not be connected.
				// This often occurs with 192.168.0.0/30 and 192.168.1.0/30 subnets.

				// localIps, _ := utils.GetLocalIPs()
				// haveLocalIP := false
				// for _, localIP := range localIps {
				// 	localIPparsed := net.ParseIP(localIP)
				// 	log.Debugf("localIP: %+v, localIPparsed: %+v", localIP, localIPparsed)
				// 	if ipv4Net.Contains(localIPparsed) {
				// 		haveLocalIP = true
				// 		log.Debugf("ip: %+v is a local IP", ip)
				// 		break
				// 	}
				// }
				// if !haveLocalIP {
				// 	log.Debugf("%s is not a local IP, skipping", ip)
				// 	continue
				// }

				serverTry := net.JoinHostPort(ip, port)
				conn, banner2, externalIP, errConn := tcp.ConnectToTCPServer(serverTry, c.Options.RelayPassword, c.Options.RoomName, 500*time.Millisecond)
				if errConn != nil {
					log.Debug(errConn)
					log.Debug("could not connect to " + serverTry)
					continue
				}
				log.Debugf("local connection established to %s", serverTry)
				log.Debugf("banner: %s", banner2)
				// reset to the local port
				banner = banner2
				c.setRelayControlAddress(serverTry)
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
	output, _ = termui.Output(os.Stderr)
	fmt.Fprint(output, "\rsecuring channel...")
	err = c.transferWithReconnect(func(attempt int) error {
		if attempt == 0 {
			return nil
		}
		return c.receiverReconnectRelayAttempt(attempt)
	})
	if err == nil {
		if c.numberOfTransferredFiles+len(c.EmptyFoldersToTransfer) == 0 {
			output, _ = termui.Output(os.Stderr)
			fmt.Fprint(output, "\rNo files transferred.\n")
		}
	} else if !isTransferDisconnectError(err) {
		c.SendError()
	}
	return
}

func (c *Client) transfer() (err error) {
	// connect to the server

	// quit with c.quit <- true
	c.quit = make(chan bool)
	attempt := &transferAttemptState{
		errc:    make(chan error, 1),
		control: c.conn[0],
	}

	// if recipient, initialize with sending pake information
	log.Debug("ready")
	if !c.Options.IsSender && !c.Step1ChannelSecured {
		c.pakeInitiator = append([]byte(nil), c.Pake.Bytes()...)
		c.pakeCurve = c.Options.Curve
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:    message.TypePAKE,
			Version: pakekey.ProtocolVersion,
			Bytes:   c.pakeInitiator,
			Bytes2:  []byte(c.Options.Curve),
		})
		if err != nil {
			return
		}
	}

	// listen for incoming messages and process them
	for {
		if e := c.ctxErr(); e != nil {
			log.Tracef("transfer: %v", e)
			err = e
			break
		}
		var data []byte
		var done bool
		data, err = c.conn[0].Receive()
		if err != nil {
			log.Debugf("got error receiving: %v", err)
			if !c.Step1ChannelSecured {
				err = fmt.Errorf("could not secure channel")
			} else if c.activeTransferStarted() {
				select {
				case reportedErr := <-attempt.errc:
					err = reportedErr
				default:
					err = transferDisconnectError{err: err}
				}
			}
			break
		}
		if bytes.Equal(data, []byte{1}) {
			log.Trace("got ping")
			continue
		}
		done, err = c.processMessage(data, attempt)
		if err != nil {
			log.Debugf("data: %s", data)
			log.Debugf("got error processing: %v", err)
			break
		}
		if done {
			break
		}
	}
	if err := c.ctxErr(); err != nil && c.SuccessfulTransfer {
		c.SuccessfulTransfer = false
		log.Tracef("SuccessfulTransfer: %v", err)
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
				if unzipErr := utils.UnzipDirectory(".", file.Name); unzipErr != nil {
					c.SuccessfulTransfer = false
					err = fmt.Errorf("failed to unzip received archive %s: %w", file.Name, unzipErr)
					log.Error(err)
					break
				}
				if removeErr := os.Remove(file.Name); removeErr != nil {
					log.Warnf("error removing %s: %v", file.Name, removeErr)
				} else {
					log.Debugf("Removing %s\n", file.Name)
				}
			}
		}
	}

	if c.Options.Stdout && !c.Options.IsSender && len(c.FilesToTransfer) > 0 && c.FilesToTransferCurrentNum < len(c.FilesToTransfer) {
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
	if err != nil && !isTransferDisconnectError(err) {
		c.SendError()
	}
	return
}

func (c *Client) createEmptyFolder(i int) (err error) {
	folderRemote, err := normalizeReceiveFolder(c.EmptyFoldersToTransfer[i].FolderRemote)
	if err != nil {
		return
	}
	c.EmptyFoldersToTransfer[i].FolderRemote = folderRemote
	if err = utils.RejectSymlinkPath(".", folderRemote); err != nil {
		return
	}
	err = os.MkdirAll(c.EmptyFoldersToTransfer[i].FolderRemote, os.ModePerm)
	if err != nil {
		return
	}
	output, colorEnabled := termui.Output(os.Stderr)
	fmt.Fprintln(output, termui.Filename(c.EmptyFoldersToTransfer[i].FolderRemote, colorEnabled))
	c.bar = c.newProgressBar(1, " ", 0)
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
	c.peerPerFileCompression = supportsFeature(senderInfo.Features, perFileCompressionFeature)
	c.Options.HashAlgorithm = senderInfo.HashAlgorithm
	c.peerReconnectVersion = senderInfo.ReconnectVersion
	c.nextReconnectRoom = senderInfo.NextReconnectRoom
	c.TotalNumberFolders = senderInfo.TotalNumberFolders
	c.FilesToTransfer, c.EmptyFoldersToTransfer, err = validateReceiveMetadata(senderInfo.FilesToTransfer, senderInfo.EmptyFoldersToTransfer)
	if err != nil {
		return true, err
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
	displayName := ""
	if len(c.FilesToTransfer) == 1 {
		displayName = c.FilesToTransfer[0].Name
		fname = quotedFilename(displayName, false)
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
	// check the totalSize does not exceed disk space
	// usage := diskusage.NewDiskUsage(".")
	// if usage.Available() < uint64(totalSize) {
	// 	return true, fmt.Errorf("not enough disk space")
	// }

	// c.spinner.Stop()
	action := "Accept"
	if c.Options.SendingText {
		action = "Display"
		fname = "text message"
		displayName = ""
	}
	if !c.Options.NoPrompt || c.Options.Ask || senderInfo.Ask {
		output, colorEnabled := termui.Output(os.Stderr)
		if displayName != "" {
			fname = quotedFilename(displayName, colorEnabled)
		}
		choicePrompt := termui.Emphasis("(Y/n)", colorEnabled)
		if c.Options.Ask || senderInfo.Ask {
			machID, _ := machineid.ID()
			fmt.Fprintf(output, "\rYour machine id is '%s'.\n%s %s (%s) from '%s'? %s ", machID, action, fname, utils.ByteCountDecimal(totalSize), senderInfo.MachineID, choicePrompt)
		} else {
			if c.TotalNumberFolders > 0 {
				fmt.Fprintf(output, "\r%s %s and %s (%s)? %s ", action, fname, folderName, utils.ByteCountDecimal(totalSize), choicePrompt)
			} else {
				fmt.Fprintf(output, "\r%s %s (%s)? %s ", action, fname, utils.ByteCountDecimal(totalSize), choicePrompt)
			}
		}
		choice, errInput := utils.GetInput("")
		choice = strings.ToLower(choice)
		if errInput != nil || (choice != "" && choice != "y" && choice != "yes") {
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
		output, colorEnabled := termui.Output(os.Stderr)
		if displayName != "" {
			fname = quotedFilename(displayName, colorEnabled)
		}
		fmt.Fprintf(output, "\rReceiving %s (%s) \n", fname, utils.ByteCountDecimal(totalSize))
	}
	output, _ := termui.Output(os.Stderr)
	fmt.Fprintf(output, "\nReceiving (<-%s)\n", c.ExternalIPConnected)

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
				output, colorEnabled := termui.Output(os.Stderr)
				fmt.Fprintf(output, "\n%s already has some content in it. \nDo you want to %s it with an empty folder? %s ",
					termui.Filename(c.EmptyFoldersToTransfer[i].FolderRemote, colorEnabled),
					termui.Warning("overwrite", colorEnabled),
					termui.Warning("(y/N)", colorEnabled),
				)
				choice, _ := utils.GetInput("")
				choice = strings.ToLower(choice)
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
		c.markTransferStarted()
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

func (c *Client) processMessagePake(m message.Message, attempt *transferAttemptState) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if c.stop.gui {
				log.Errorf("panic: %v", r)
				c.stop.Cancel()
			} else {
				panic(r)
			}
		}
	}()
	log.Debug("received pake payload")
	if m.Version != pakekey.ProtocolVersion {
		return incompatiblePakeVersionError{got: m.Version}
	}
	if c.pakeConfirmationPending || c.Key != nil {
		return pakeHandshakeError{err: fmt.Errorf("unexpected duplicate PAKE payload")}
	}

	var salt []byte
	if c.Options.IsSender {
		// initialize curve based on the recipient's choice
		c.pakeCurve = string(m.Bytes2)
		log.Debugf("using curve %s", c.pakeCurve)
		c.Pake, err = pakekey.Init(
			[]byte(c.pakePassphrase),
			1,
			c.pakeCurve,
			pakekey.PurposeTransfer,
			c.Options.RoomName,
		)
		if err != nil {
			log.Error(err)
			return pakeHandshakeError{err: err}
		}

		// update the pake
		c.pakeInitiator = append([]byte(nil), m.Bytes...)
		err = c.Pake.Update(m.Bytes)
		if err != nil {
			return pakeHandshakeError{err: err}
		}
		c.pakeResponder = append([]byte(nil), c.Pake.Bytes()...)

		// generate salt and send it back to recipient
		log.Debug("generating salt")
		salt = make([]byte, pakekey.SaltSize)
		if _, rerr := rand.Read(salt); rerr != nil {
			log.Errorf("can't generate random numbers: %v", rerr)
			return pakeHandshakeError{err: rerr}
		}
		if err = c.derivePakeKeys(salt); err != nil {
			return pakeHandshakeError{err: err}
		}
		c.pakeConfirmationPending = true
		log.Debug("sender sending pake+salt")
		err = message.Send(c.conn[0], nil, message.Message{
			Type:    message.TypePAKE,
			Version: pakekey.ProtocolVersion,
			Bytes:   c.pakeResponder,
			Bytes2:  salt,
		})
		if err != nil {
			return pakeHandshakeError{err: err}
		}
	} else {
		if len(c.pakeInitiator) == 0 || c.pakeCurve == "" {
			return pakeHandshakeError{err: fmt.Errorf("PAKE response arrived before initialization")}
		}
		if len(m.Bytes2) != pakekey.SaltSize {
			return pakeHandshakeError{err: fmt.Errorf("invalid PAKE salt length %d", len(m.Bytes2))}
		}
		c.pakeResponder = append([]byte(nil), m.Bytes...)
		err = c.Pake.Update(m.Bytes)
		if err != nil {
			return pakeHandshakeError{err: err}
		}
		salt = append([]byte(nil), m.Bytes2...)
		if err = c.derivePakeKeys(salt); err != nil {
			return pakeHandshakeError{err: err}
		}
		c.pakeConfirmationPending = true
		err = message.Send(c.conn[0], nil, message.Message{
			Type:    message.TypePAKEConfirm,
			Version: pakekey.ProtocolVersion,
			Bytes:   c.pakeKeys.ConfirmationA,
		})
		if err != nil {
			return pakeHandshakeError{err: err}
		}
	}
	return nil
}

func (c *Client) derivePakeKeys(salt []byte) error {
	sharedKey, err := c.Pake.SessionKey()
	if err != nil {
		return err
	}
	c.pakeKeys, err = pakekey.Derive(sharedKey, pakekey.Context{
		Purpose:   pakekey.PurposeTransfer,
		Room:      c.Options.RoomName,
		Curve:     c.pakeCurve,
		Initiator: c.pakeInitiator,
		Responder: c.pakeResponder,
		Salt:      salt,
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) processMessagePakeConfirm(m message.Message, attempt *transferAttemptState) error {
	if m.Version != pakekey.ProtocolVersion {
		return incompatiblePakeVersionError{got: m.Version}
	}
	if !c.pakeConfirmationPending || len(c.pakeKeys.EncryptionKey) == 0 || c.Key != nil {
		return pakeHandshakeError{err: fmt.Errorf("unexpected PAKE confirmation")}
	}

	if c.Options.IsSender {
		if !pakekey.Confirm(c.pakeKeys.ConfirmationA, m.Bytes) {
			return pakeHandshakeError{err: fmt.Errorf("recipient PAKE confirmation failed")}
		}
		if err := message.Send(c.conn[0], nil, message.Message{
			Type:    message.TypePAKEConfirm,
			Version: pakekey.ProtocolVersion,
			Bytes:   c.pakeKeys.ConfirmationB,
		}); err != nil {
			return pakeHandshakeError{err: err}
		}
	} else if !pakekey.Confirm(c.pakeKeys.ConfirmationB, m.Bytes) {
		return pakeHandshakeError{err: fmt.Errorf("sender PAKE confirmation failed")}
	}

	c.Key = append([]byte(nil), c.pakeKeys.EncryptionKey...)
	dataAEAD, err := crypt.NewAESGCM(c.Key)
	if err != nil {
		return pakeHandshakeError{err: err}
	}
	c.dataAEAD = dataAEAD
	c.pakeKeys = pakekey.Keys{}
	c.pakeConfirmationPending = false
	return c.activateSecureChannel(attempt)
}

func (c *Client) activateSecureChannel(attempt *transferAttemptState) (err error) {
	log.Debug("PAKE key confirmation succeeded")

	// connects to the other ports of the server for transfer
	var wg sync.WaitGroup
	relayControlAddress := c.currentRelayControlAddress()
	if relayControlAddress == "" {
		relayControlAddress = c.Options.RelayAddress
	}
	relayControlAddress = normalizeRelayAddress(relayControlAddress)
	relayHost, _, err := net.SplitHostPort(relayControlAddress)
	if err != nil {
		return fmt.Errorf("bad relay address %s: %w", relayControlAddress, err)
	}

	if need := len(c.Options.RelayPorts) + 1; len(c.conn) < need {
		newConn := make([]*comm.Comm, need)
		copy(newConn, c.conn)
		c.conn = newConn
	}

	errc := make(chan error, len(c.Options.RelayPorts))
	wg.Add(len(c.Options.RelayPorts))
	for i := 0; i < len(c.Options.RelayPorts); i++ {
		log.Debugf("port: [%s]", c.Options.RelayPorts[i])
		go func(j int) {
			defer wg.Done()
			server := net.JoinHostPort(relayHost, c.Options.RelayPorts[j])
			log.Debugf("connecting to %s", server)
			dataConn, _, _, connErr := tcp.ConnectToTCPServer(
				server,
				c.Options.RelayPassword,
				fmt.Sprintf("%s-%d", c.Options.RoomName, j),
			)
			if connErr != nil {
				errc <- connErr
				return
			}
			c.conn[j+1] = dataConn
			log.Debugf("connected to %s", server)
			if !c.Options.IsSender {
				go c.receiveData(j, c.conn[j+1], attempt)
			}
		}(i)
	}
	wg.Wait()
	close(errc)
	for connectErr := range errc {
		if connectErr != nil {
			return fmt.Errorf("%w: could not connect transfer ports: %v", ErrRelayConnection, connectErr)
		}
	}
	if !c.Options.IsSender {
		log.Debug("sending external IP")
		err = message.Send(c.conn[0], c.Key, message.Message{
			Type:    message.TypeExternalIP,
			Message: c.ExternalIP,
			Bytes:   c.pakeResponder,
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

func (c *Client) processMessage(payload []byte, attempt *transferAttemptState) (done bool, err error) {
	m, err := message.Decode(c.Key, payload)
	if err != nil {
		err = fmt.Errorf("problem with decoding: %w", err)
		log.Debug(err)
		return
	}

	// Only PAKE setup and confirmation messages may be unencrypted.
	if m.Type != message.TypePAKE && m.Type != message.TypePAKEConfirm && c.Key == nil {
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
		err = c.processMessagePake(m, attempt)
		if err != nil {
			log.Debug(err)
		}
	case message.TypePAKEConfirm:
		err = c.processMessagePakeConfirm(m, attempt)
		if err != nil {
			log.Debug(err)
		}
	case message.TypeExternalIP:
		done, err = c.processExternalIP(m)
	case message.TypeError:
		// c.spinner.Stop()
		log.Trace("Peer initiates interruption of my loops and goroutines")
		c.stop.Cancel()
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
		c.peerReconnectVersion = remoteFile.ReconnectVersion
		c.peerPerFileCompression = supportsFeature(remoteFile.Features, perFileCompressionFeature)
		c.FilesToTransferCurrentNum = remoteFile.FilesToTransferCurrentNum
		c.CurrentFileChunkRanges = remoteFile.CurrentFileChunkRanges
		c.CurrentFileChunkCount = utils.ChunkRangesCount(
			c.CurrentFileChunkRanges,
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
			models.TCP_BUFFER_SIZE/2,
		)
		log.Debugf("current file has %d requested chunks", c.CurrentFileChunkCount)
		c.Step3RecipientRequestFile = true
		c.markTransferStarted()

		if c.Options.Ask {
			output, colorEnabled := termui.Output(os.Stderr)
			fmt.Fprintf(output, "Send to machine '%s'? %s ",
				remoteFile.MachineID,
				termui.Emphasis("(Y/n)", colorEnabled),
			)
			choice, errInput := utils.GetInput("")
			choice = strings.ToLower(choice)
			if errInput != nil || (choice != "" && choice != "y" && choice != "yes") {
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
	err = c.updateState(attempt)
	if err != nil {
		log.Debugf("got error from updating state: %v", err)
		return
	}
	return
}

func (c *Client) updateIfSenderChannelSecured() (err error) {
	if c.Options.IsSender && c.Step1ChannelSecured && !c.Step2FileInfoTransferred {
		if c.filesReady != nil {
			select {
			case <-c.filesReady:
				if c.filesReadyErr != nil {
					return c.filesReadyErr
				}
			case <-c.stop.ctx.Done():
				return c.stop.ctx.Err()
			}
		}
		var b []byte
		machID, _ := machineid.ID()
		nextReconnectRoom := ""
		if c.reconnectVersion >= ReconnectVersion {
			nextReconnectRoom, err = generateReconnectRoom()
			if err != nil {
				return
			}
			c.nextReconnectRoom = nextReconnectRoom
		}
		b, err = json.Marshal(SenderInfo{
			FilesToTransfer:        c.FilesToTransfer,
			EmptyFoldersToTransfer: c.EmptyFoldersToTransfer,
			MachineID:              machID,
			Ask:                    c.Options.Ask,
			TotalNumberFolders:     c.TotalNumberFolders,
			SendingText:            c.Options.SendingText,
			NoCompress:             c.Options.NoCompress,
			HashAlgorithm:          c.Options.HashAlgorithm,
			ReconnectVersion:       c.reconnectVersion,
			NextReconnectRoom:      nextReconnectRoom,
			Features:               []string{perFileCompressionFeature},
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
	folderRemote, pathToFile, err := normalizeReceiveFilePath(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote,
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Name,
	)
	if err != nil {
		return
	}
	c.FilesToTransfer[c.FilesToTransferCurrentNum].FolderRemote = folderRemote
	c.FilesToTransfer[c.FilesToTransferCurrentNum].Name = path.Base(pathToFile)
	folderForFile, _ := filepath.Split(pathToFile)
	folderForFileBase := filepath.Base(folderForFile)
	if err = utils.RejectSymlinkPath(".", folderForFile); err != nil {
		return
	}
	if folderForFileBase != "." && folderForFileBase != "" {
		if err := os.MkdirAll(folderForFile, os.ModePerm); err != nil {
			log.Errorf("can't create %s: %v", folderForFile, err)
		}
	}
	var errOpen error
	if err = rejectSymlinkDestination(pathToFile); err != nil {
		return
	}
	c.CurrentFile, errOpen = os.OpenFile(
		pathToFile,
		os.O_WRONLY, 0o666)
	var truncate bool // default false
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
		if err = rejectSymlinkDestination(pathToFile); err != nil {
			return
		}
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
			return
		}
		c.SuccessfulTransfer = true
		c.FilesHasFinished[c.FilesToTransferCurrentNum] = struct{}{}
		return
	}

	err = c.recipientInitializeFile()
	if err != nil {
		return
	}

	c.receiveMutex.Lock()
	c.TotalSent = 0
	c.TotalChunksTransferred = 0
	c.CurrentFileIsClosed = false
	c.receiveMutex.Unlock()
	machID, _ := machineid.ID()
	bRequest, _ := json.Marshal(RemoteFileRequest{
		CurrentFileChunkRanges:    c.CurrentFileChunkRanges,
		FilesToTransferCurrentNum: c.FilesToTransferCurrentNum,
		MachineID:                 machID,
		ReconnectVersion:          c.reconnectVersion,
		Features:                  []string{perFileCompressionFeature},
	})
	c.CurrentFileChunkCount = utils.ChunkRangesCount(
		c.CurrentFileChunkRanges,
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		models.TCP_BUFFER_SIZE/2,
	)

	if !finished {
		// setup the progressbar
		c.setBar()
	}

	log.Debugf("sending recipient ready with %d chunks", c.CurrentFileChunkCount)
	err = message.Send(c.conn[0], c.Key, message.Message{
		Type:  message.TypeRecipientReady,
		Bytes: bRequest,
	})
	if err != nil {
		return
	}
	c.Step3RecipientRequestFile = true
	c.markTransferStarted()
	return
}

func formatDescription(description string) string {
	const (
		// Reserve extra room for variable progress metadata such as [elapsed:remaining].
		progressMetaWidth = 78
		minDescription    = 12
		defaultTermWidth  = 80
	)

	width, _, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || width <= 0 {
		width, _, err = term.GetSize(int(os.Stdout.Fd()))
	}
	if err != nil || width <= 0 {
		if envColumns, convErr := strconv.Atoi(os.Getenv("COLUMNS")); convErr == nil && envColumns > 0 {
			width = envColumns
		} else {
			width = defaultTermWidth
		}
	}

	maxDescription := width - progressMetaWidth
	if maxDescription < minDescription {
		maxDescription = minDescription
	}

	runes := []rune(description)
	if len(runes) > maxDescription {
		if maxDescription <= 3 {
			return string(runes[:maxDescription])
		}
		return string(runes[:maxDescription-3]) + "..."
	}
	return description
}

func (c *Client) createEmptyFileAndFinish(fileInfo FileInfo, i int) (err error) {
	log.Debugf("touching file with folder / name")
	folderRemote, pathToFile, err := normalizeReceiveFilePath(fileInfo.FolderRemote, fileInfo.Name)
	if err != nil {
		return
	}
	fileInfo.FolderRemote = folderRemote
	fileInfo.Name = path.Base(pathToFile)
	if err = utils.RejectSymlinkPath(".", filepath.Dir(pathToFile)); err != nil {
		return
	}
	if !utils.Exists(fileInfo.FolderRemote) {
		err = os.MkdirAll(fileInfo.FolderRemote, os.ModePerm)
		if err != nil {
			log.Error(err)
			return
		}
	}
	if fileInfo.Symlink != "" {
		if err = validateReceiveSymlinkTarget(fileInfo.FolderRemote, fileInfo.Symlink); err != nil {
			return
		}
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
		if err = rejectSymlinkDestination(pathToFile); err != nil {
			return
		}
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
	c.bar = c.newProgressBar(1, formatDescription(description), 0)
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
			fileHash, errHash = utils.HashFile(path.Join(fileInfo.FolderRemote, fileInfo.Name), c.Options.HashAlgorithm, !c.Options.SendingText)
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
			if errHash == nil && errRecipientFile == nil && !strings.HasPrefix(fileInfo.Name, "croc-stdin-") && !c.Options.SendingText && c.Options.Rename {
				newName := utils.UnusedFilename(fileInfo.FolderRemote, fileInfo.Name)
				output, colorEnabled := termui.Output(os.Stderr)
				fmt.Fprintf(output, "Receiving %s as %s\n", quotedFilename(fileInfo.Name, colorEnabled), quotedFilename(newName, colorEnabled))
				c.FilesToTransfer[i].Name = newName
				fileInfo.Name = newName
			}
			if errHash == nil && !c.Options.Overwrite && !c.Options.Rename && errRecipientFile == nil && !strings.HasPrefix(fileInfo.Name, "croc-stdin-") && !c.Options.SendingText {

				missingRanges := utils.MissingChunks(
					path.Join(fileInfo.FolderRemote, fileInfo.Name),
					fileInfo.Size,
					models.TCP_BUFFER_SIZE/2,
				)
				missingBytes := utils.ChunkRangesBytes(
					missingRanges,
					fileInfo.Size,
					models.TCP_BUFFER_SIZE/2,
				)
				percentDone := 100 - float64(missingBytes)/float64(fileInfo.Size)*100

				log.Debug("asking to overwrite")
				action := "Overwrite"
				promptDetail := ""
				promptSpacing := " "
				if percentDone < 99 {
					action = "Resume"
					promptDetail = fmt.Sprintf(" (%2.1f%%)", percentDone)
					promptSpacing = "   "
				}
				output, colorEnabled := termui.Output(os.Stderr)
				styledAction := termui.Warning(action, colorEnabled)
				styledChoice := termui.Warning("(y/N)", colorEnabled)
				if action == "Resume" {
					styledAction = action
					styledChoice = termui.Emphasis("(y/N)", colorEnabled)
				}
				fmt.Fprintf(output, "\n%s %s%s? %s%s(use --overwrite to omit) ",
					styledAction,
					quotedFilename(path.Join(fileInfo.FolderRemote, fileInfo.Name), colorEnabled),
					promptDetail,
					styledChoice,
					promptSpacing,
				)
				choice, _ := utils.GetInput("")
				choice = strings.ToLower(choice)
				if choice != "y" && choice != "yes" {
					fmt.Fprintf(output, "Skipping %s\n", quotedFilename(path.Join(fileInfo.FolderRemote, fileInfo.Name), colorEnabled))
					continue
				}
			}
		} else {
			log.Debugf("hashes are equal %x == %x", fileHash, fileInfo.Hash)

			if !fileInfo.ModTime.IsZero() {
				if err := os.Chtimes(path.Join(fileInfo.FolderRemote, fileInfo.Name), fileInfo.ModTime, fileInfo.ModTime); err != nil {
					log.Warnf("chtimes %v: %v", fileInfo.ModTime, err)
				} else {
					log.Debugf("chtimes %v", fileInfo.ModTime)
				}
			}
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
				output, colorEnabled := termui.Output(os.Stderr)
				fmt.Fprintf(output, "\r%s\n", termui.Filename(newFolder, colorEnabled))
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
		output, colorEnabled := termui.Output(os.Stderr)
		fmt.Fprintln(output, termui.Success(fmt.Sprintf(" %d/%d", c.finishedNum, c.TotalNumberOfContents), colorEnabled))
	} else {
		fmt.Fprintf(os.Stderr, "\n")
	}
}

func (c *Client) updateState(attempt *transferAttemptState) (err error) {
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
			output, _ := termui.Output(os.Stderr)
			fmt.Fprintf(output, "\nSending (->%s)\n", c.ExternalIPConnected)
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

					c.bar = c.newProgressBar(1, formatDescription(description), 0)
					c.bar.Finish()
				}
			}
		}
		c.Step4FileTransferred = true
		c.markTransferStarted()
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
			go c.sendData(i, c.conn[i+1], c.fread, attempt)
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
	c.bar = c.newProgressBar(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		formatDescription(description),
		100*time.Millisecond,
	)
	byteToDo := utils.ChunkRangesBytes(
		c.CurrentFileChunkRanges,
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		models.TCP_BUFFER_SIZE/2,
	)
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

func (c *Client) receiveData(i int, dataConn *comm.Comm, attempt *transferAttemptState) {
	defer func() {
		if r := recover(); r != nil {
			attempt.report(fmt.Errorf("receive data panic: %v", r))
		}
	}()
	log.Tracef("%d receiving data", i)
	var receiveBuffer []byte
	var decompressedBuffer []byte
	for {
		data, err := dataConn.ReceiveInto(receiveBuffer)
		if err != nil {
			if c.activeTransferStarted() && c.ctxErr() == nil {
				attempt.report(transferDisconnectError{err: err})
			}
			return
		}
		receiveBuffer = data
		if bytes.Equal(data, []byte{1}) {
			log.Trace("got ping")
			continue
		}

		if c.dataAEAD == nil {
			attempt.report(fmt.Errorf("data cipher is not initialized"))
			return
		}
		data, err = crypt.DecryptAEADInPlace(data, c.dataAEAD)
		if err != nil {
			attempt.report(err)
			return
		}
		if c.currentFileUsesCompression() {
			data, err = compress.DecompressTo(decompressedBuffer, data, maxDecompressedChunkSize)
			if err != nil {
				attempt.report(fmt.Errorf("decompress data chunk: %w", err))
				return
			}
			decompressedBuffer = data
		}
		if len(data) < 9 || len(data) > maxDecompressedChunkSize {
			attempt.report(fmt.Errorf("invalid data chunk size: %d", len(data)))
			return
		}

		// get position
		position := binary.LittleEndian.Uint64(data[:8])
		positionInt64 := int64(position)

		c.receiveMutex.Lock()
		if c.CurrentFileIsClosed || c.CurrentFile == nil {
			c.receiveMutex.Unlock()
			log.Tracef("was closed %d", i)
			return
		}
		if err := c.ctxErr(); err != nil {
			c.CurrentFileIsClosed = true
			file := c.CurrentFile
			c.receiveMutex.Unlock()
			log.Tracef("stopping: %v", err)
			if err := file.Close(); err != nil {
				log.Tracef("closing %s: %v", file.Name(), err)
			} else {
				log.Tracef("Successful closing %s", file.Name())
			}
			log.Tracef("sending close-sender")
			if sendErr := message.Send(c.conn[0], c.Key, message.Message{
				Type: message.TypeCloseSender,
			}); sendErr != nil {
				log.Tracef("sending close-sender: %v", sendErr)
			}
			return
		}
		receiveFile := c.CurrentFile
		c.receiveMutex.Unlock()

		// os.File supports concurrent WriteAt calls. Keep disk I/O outside the
		// state lock so all relay connections can write in parallel.
		_, err = receiveFile.WriteAt(data[8:], positionInt64)
		if err != nil {
			attempt.report(err)
			return
		}

		c.receiveMutex.Lock()
		if c.CurrentFileIsClosed || c.CurrentFile != receiveFile {
			c.receiveMutex.Unlock()
			return
		}
		c.TotalSent += int64(len(data[8:]))
		c.TotalChunksTransferred++
		finished := c.TotalChunksTransferred == c.CurrentFileChunkCount ||
			c.TotalSent == c.FilesToTransfer[c.FilesToTransferCurrentNum].Size
		if finished {
			c.CurrentFileIsClosed = true
		}
		c.receiveMutex.Unlock()

		c.bar.Add(len(data[8:]))
		if finished {
			log.Debug("finished receiving!")
			if err = receiveFile.Close(); err != nil {
				log.Debugf("error closing %s: %v", receiveFile.Name(), err)
			} else {
				log.Debugf("Successful closing %s", receiveFile.Name())
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
				if c.ctxErr() == nil {
					attempt.report(transferDisconnectError{err: err})
				}
				return
			}
		}
	}
}

func (c *Client) sendData(i int, dataConn *comm.Comm, fread *os.File, attempt *transferAttemptState) {
	defer func() {
		if r := recover(); r != nil {
			attempt.report(fmt.Errorf("send data panic: %v", r))
		}
		log.Debugf("finished with %d", i)
		attempt.finishSenderData(len(c.Options.RelayPorts), fread)
	}()

	chunkSize := int64(models.TCP_BUFFER_SIZE / 2)
	connectionCount := int64(len(c.Options.RelayPorts))
	readingPos := int64(i) * chunkSize
	pos := uint64(readingPos)
	stride := chunkSize * connectionCount
	fileSize := c.FilesToTransfer[c.FilesToTransferCurrentNum].Size
	payload := make([]byte, 8+chunkSize)
	var encryptedBuffer []byte
	var compressedBuffer []byte
	for readingPos < fileSize {
		if err := c.ctxErr(); err != nil {
			log.Tracef("stopping send %d: %v", i, err)
			return
		}
		// Skip disk I/O entirely for chunks the recipient already has.
		usableChunk := utils.ChunkRangesContain(c.CurrentFileChunkRanges, int64(pos))
		if !usableChunk {
			readingPos += stride
			pos += uint64(stride)
			continue
		}

		n, errRead := fread.ReadAt(payload[8:], readingPos)
		if c.limiter != nil {
			r := c.limiter.ReserveN(time.Now(), n)
			log.Debugf("Limiting Upload for %d", r.Delay())
			time.Sleep(r.Delay())
		}
		if n > 0 {
			binary.LittleEndian.PutUint64(payload[:8], pos)
			plain := payload[:8+n]
			var dataToSend []byte
			var err error
			if c.currentFileUsesCompression() {
				compressedBuffer = compress.CompressTo(compressedBuffer, plain)
				dataToSend, err = crypt.EncryptAEADTo(encryptedBuffer, compressedBuffer, c.dataAEAD)
			} else {
				dataToSend, err = crypt.EncryptAEADTo(encryptedBuffer, plain, c.dataAEAD)
			}
			if err != nil {
				attempt.report(err)
				return
			}
			encryptedBuffer = dataToSend
			if err = dataConn.Send(dataToSend); err != nil {
				if c.ctxErr() == nil {
					attempt.report(transferDisconnectError{err: err})
				}
				return
			}
			c.bar.Add(n)
			c.mutex.Lock()
			c.TotalSent += int64(n)
			c.mutex.Unlock()
		}
		readingPos += stride
		pos += uint64(stride)

		if errRead != nil {
			if errRead == io.EOF {
				break
			}
			attempt.report(errRead)
			return
		}
	}
}

// isExecutableInPath checks for the availability of an executable
func isExecutableInPath(executableName string) bool {
	_, err := exec.LookPath(executableName)
	return err == nil
}

// copyToClipboard tries to send the code to the operating system clipboard
func copyToClipboard(str string, quiet bool, extendedClipboard bool) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	// Windows should always have clip.exe in PATH by default
	case "windows":
		cmd = exec.Command("clip")
	// MacOS uses pbcopy
	case "darwin":
		cmd = exec.Command("pbcopy")
	// These Unix-like systems are likely using Xorg(with xclip or xsel) or Wayland(with wl-copy or waycopy)
	case "linux", "android", "hurd", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "illumos", "plan9":
		if os.Getenv("XDG_SESSION_TYPE") == "wayland" { // Wayland running
			if isExecutableInPath("wl-copy") {
				cmd = exec.Command("wl-copy")
			} else if isExecutableInPath("waycopy") {
				cmd = exec.Command("waycopy")
			}
		} else if os.Getenv("XDG_SESSION_TYPE") == "x11" || os.Getenv("XDG_SESSION_TYPE") == "xorg" { // Xorg running
			if isExecutableInPath("xclip") {
				cmd = exec.Command("xclip", "-selection", "clipboard")
			} else if isExecutableInPath("xsel") {
				cmd = exec.Command("xsel", "-b")
			}
		} else if isExecutableInPath("termux-clipboard-set") {
			cmd = exec.Command("termux-clipboard-set")
		}
	default:
		return
	}
	// Nothing has been found
	if cmd == nil {
		return
	}
	// Sending stdin into the available clipboard program
	cmd.Stdin = bytes.NewReader([]byte(str))
	if err := cmd.Run(); err != nil {
		log.Debugf("error copying to clipboard: %v", err)
		return
	}
	if !quiet {
		output, colorEnabled := termui.Output(os.Stderr)
		if extendedClipboard {
			fmt.Fprintln(output, termui.Success("Command copied to clipboard!", colorEnabled))
		} else {
			fmt.Fprintln(output, termui.Success("Code copied to clipboard!", colorEnabled))
		}
	}
}

// CopyToClipboard copies a croc share value using the platform clipboard helper.
func CopyToClipboard(value string, quiet bool, extended bool) {
	copyToClipboard(value, quiet, extended)
}
