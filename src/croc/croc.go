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
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/denisbrodbeck/machineid"
	ignore "github.com/sabhiram/go-gitignore"
	log "github.com/schollz/croc/v11/src/logger"
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
	"github.com/schollz/croc/v11/src/receivefs"
	"github.com/schollz/croc/v11/src/redact"
	"github.com/schollz/croc/v11/src/tailcattransport"
	"github.com/schollz/croc/v11/src/tcp"
	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/croc/v11/src/utils"
)

var (
	ipRequest        = []byte("ips?")
	handshakeRequest = []byte("handshake")

	alternateSenderRouteTimeout = 10 * time.Second
)

func encryptLocalProbePayload(key, plaintext []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("local probe channel is not authenticated")
	}
	return crypt.Encrypt(plaintext, key)
}

func decryptLocalProbePayload(key, ciphertext []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("local probe channel is not authenticated")
	}
	return crypt.Decrypt(ciphertext, key)
}

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
	HashExplicit      bool `json:"-"`
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
	Transport         TransportMode `json:",omitempty"`
}

// TransportMode controls how a sender selects the file-data connection after
// the PAKE-authenticated control channel is established. Receivers must use
// TransportAuto and follow the sender's negotiated selection.
type TransportMode string

const (
	TransportAuto  TransportMode = "auto"
	TransportDERP  TransportMode = "derp"
	TransportRelay TransportMode = "relay"
)

// ParseTransportMode validates and normalizes a transport name. An empty value
// is the API-compatible spelling of the default auto mode.
func ParseTransportMode(value string) (TransportMode, error) {
	mode := TransportMode(strings.ToLower(strings.TrimSpace(value)))
	if mode == "" {
		mode = TransportAuto
	}
	switch mode {
	case TransportAuto, TransportDERP, TransportRelay:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid transport %q (choose auto, derp, or relay)", value)
	}
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
	startup                         startupTiming

	// steps involved in forming relationship
	lifecycleMu               sync.RWMutex
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
	connectionsMu              sync.RWMutex
	conn                       []*comm.Comm
	baseRoomName               string
	pakePassphrase             string
	pakeInitiator              []byte
	pakeResponder              []byte
	pakeCurve                  string
	pakeKeys                   pakekey.Keys
	pakeConfirmationPending    bool
	nextReconnectRoom          string
	relayControlAddress        string
	relayCapability            string
	reconnectRelayAddresses    []string
	reconnectRelayMu           sync.Mutex
	reconnectVersion           int
	peerReconnectVersion       int
	peerPerFileCompression     bool
	peerInlineMetadata         bool
	peerProgressiveHash        bool
	peerStagedTransport        bool
	peerImplicitTailcatReady   bool
	tailcat                    tailcatClientState
	transportSelectionReceived bool
	selectedDataTransport      atomic.Int32
	senderRouteReady           chan struct{}
	filesReady                 chan struct{}
	filesReadyErr              error
	filesReadyOnce             sync.Once
	senderRouteReadyOnce       sync.Once
	externalIPReady            chan struct{}
	externalIPReadyOnce        sync.Once
	transferStarted            atomic.Bool
	// localRelayPort is the control port of the ephemeral local relay started by
	// setupLocalRelay(). It is captured before any goroutines that might
	// overwrite c.Options.RelayPorts are launched.
	localRelayPort string

	barMu              sync.RWMutex
	bar                *progressbar.ProgressBar
	longestFilename    int
	firstSend          bool
	receiveStatusWidth int

	mutex                    *sync.Mutex
	receiveMutex             *sync.Mutex
	receiveRootMu            sync.Mutex
	receiveRoot              *receivefs.Root
	fread                    *os.File
	numfinished              int
	quit                     chan bool
	finishedNum              int
	numberOfTransferredFiles int
	numberOfUnchangedFiles   int
	preparedHashAlgorithm    string
	sourceSnapshots          []os.FileInfo
	filePreparationMu        sync.Mutex
	remainingPreparationOnce sync.Once
	preparationErr           atomic.Value
	exactHashMu              sync.Mutex
	exactHashPending         int
	exactHashLocal           []byte
	exactHashResults         map[int]bool
	senderDataMu             sync.Mutex
	senderChunkQueue         *requestedChunkQueue
	senderDataAttempt        *transferAttemptState
	senderDataFile           *os.File
	senderDataWorkers        map[*comm.Comm]struct{}
	senderWorkerSequence     int
	relayStandbyMu           sync.Mutex
	relayStandbyReady        bool
	stagedRelayDelayOverride time.Duration
	stagedSelectionOverride  time.Duration
	stagedRelayOpen          func(*transferAttemptState, []int, bool) error

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
	ModTime      time.Time   `json:"m,omitzero"`
	IsCompressed bool        `json:"c"`
	IsEncrypted  bool        `json:"e,omitempty"`
	Symlink      string      `json:"sy,omitempty"`
	Mode         os.FileMode `json:"md,omitempty"`
	TempFile     bool        `json:"tf,omitempty"`
	IsIgnored    bool        `json:"ig,omitempty"`
	Prepared     bool        `json:"p,omitempty"`
}

type FilePrepared struct {
	Index        int    `json:"i"`
	Hash         []byte `json:"h"`
	IsCompressed bool   `json:"c"`
}

// RemoteFileRequest requests specific bytes
type RemoteFileRequest struct {
	CurrentFileChunkRanges    []int64
	FilesToTransferCurrentNum int
	MachineID                 string
	ExternalIP                string `json:",omitempty"`
	ReconnectVersion          int
	Features                  []string `json:",omitempty"`
}

// SenderInfo lists the files to be transferred
type SenderInfo struct {
	FilesToTransfer        []FileInfo
	EmptyFoldersToTransfer []FileInfo
	TotalNumberFolders     int
	MachineID              string
	ExternalIP             string `json:",omitempty"`
	Ask                    bool
	SendingText            bool
	NoCompress             bool
	HashAlgorithm          string
	ReconnectVersion       int
	NextReconnectRoom      string
	Features               []string `json:",omitempty"`
}

const (
	perFileCompressionFeature   = "per-file-compression-v1"
	inlinePeerMetadataFeature   = "inline-peer-metadata-v1"
	progressiveFileHashFeature  = "progressive-file-hash-v1"
	stagedTransportFeature      = "staged-transport-v1"
	implicitTailcatReadyFeature = "implicit-tailcat-ready-v1"
	selectedTransportUnset      = 0
	selectedTransportRelay      = 2
	localProbeResponseTimeout   = 500 * time.Millisecond
)

// ErrRelayConnection marks a failure to establish a relay control or data
// connection. Callers may use it to invalidate cached relay selections without
// treating peer or transfer failures as relay availability failures.
var (
	ErrRelayConnection = errors.New("relay connection failed")
	// ErrDERPConnection is the compatibility sentinel for failures in the
	// Tailcat-backed data path selected by --transport derp.
	ErrDERPConnection = errors.New("DERP connection failed")
)

func supportsFeature(features []string, wanted string) bool {
	return slices.Contains(features, wanted)
}

// New establishes a new connection for transferring files between two instances.
func New(ops Options) (*Client, error) {
	return newClient(ops, defaultTailcatDataTransport())
}

func newClient(ops Options, transport tailcatDataTransport) (c *Client, err error) {
	defer func() { err = redact.Error(err, ops.SharedSecret) }()
	c = new(Client)
	c.startup.start()
	c.exactHashPending = -1
	c.exactHashResults = make(map[int]bool)
	if transport == nil {
		transport = defaultTailcatDataTransport()
	}
	c.tailcat.transport = transport
	c.FilesHasFinished = make(map[int]struct{})

	// setup basic info
	c.Options = ops
	c.Options.Transport, err = ParseTransportMode(string(c.Options.Transport))
	if err != nil {
		return nil, err
	}
	if !c.Options.IsSender && c.Options.Transport != TransportAuto {
		return nil, errors.New("transport selection is sender-only")
	}
	Debug(c.Options.Debug)
	if c.Options.Transport != TransportAuto && c.Options.OnlyLocal {
		return nil, errors.New("--transport cannot be combined with --local unless it is auto")
	}
	if c.Options.Transport == TransportDERP {
		if c.Options.ShowQrCode {
			return nil, errors.New("--transport derp cannot be combined with --qrcode")
		}
		if !c.dataTransport().Available() {
			return nil, fmt.Errorf("%w: %w", ErrDERPConnection, tailcattransport.ErrUnsupported)
		}
	}

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
	c.filesReady = make(chan struct{})

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
	c.externalIPReady = make(chan struct{})
	c.stop = newStop(context.Background())
	return
}

func (c *Client) redactError(err error) error {
	return redact.Error(
		err,
		c.Options.SharedSecret,
		c.pakePassphrase,
		c.baseRoomName,
		c.Options.RoomName,
		c.nextReconnectRoom,
	)
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

type tailcatProtocolError struct {
	err error
}

func (e tailcatProtocolError) Error() string { return e.err.Error() }
func (e tailcatProtocolError) Unwrap() error { return e.err }

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
	tailcat tailcatAttemptState

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
	if slices.Contains(c.reconnectRelayAddresses, address) {
		return
	}
	c.reconnectRelayAddresses = append(c.reconnectRelayAddresses, address)
}

func (c *Client) setRelayControlAddress(address string) {
	c.reconnectRelayMu.Lock()
	capability := c.relayCapability
	c.reconnectRelayMu.Unlock()
	c.setRelayControlRoute(address, capability)
}

func (c *Client) setRelayControlRoute(address, capability string) {
	address = normalizeRelayAddress(address)
	if address == "" {
		return
	}
	c.reconnectRelayMu.Lock()
	defer c.reconnectRelayMu.Unlock()
	c.relayControlAddress = address
	c.relayCapability = capability
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
		seen := slices.Contains(candidates, address)
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

func (c *Client) currentRelayControlRoute() (address, capability string) {
	c.reconnectRelayMu.Lock()
	defer c.reconnectRelayMu.Unlock()
	return c.relayControlAddress, c.relayCapability
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

func (c *Client) markExternalIPReady() {
	if c.externalIPReady == nil {
		return
	}
	c.externalIPReadyOnce.Do(func() {
		close(c.externalIPReady)
	})
}

func (c *Client) waitForExternalIP() {
	if c.externalIPReady != nil {
		<-c.externalIPReady
	}
}

func (c *Client) canRetryTransfer(err error, attempt int) bool {
	if err == nil || c.lifecycleSnapshot().Successful {
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
	for _, conn := range c.connectionsSnapshot() {
		if conn != nil {
			conn.Close()
		}
	}
	c.closeTailcatBundle()
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
	c.resetLifecycle()
	c.Key = nil
	c.dataAEAD = nil
	c.pakeInitiator = nil
	c.pakeResponder = nil
	c.pakeCurve = ""
	c.pakeKeys = pakekey.Keys{}
	c.pakeConfirmationPending = false
	c.peerPerFileCompression = false
	c.peerInlineMetadata = false
	c.peerProgressiveHash = false
	c.peerStagedTransport = false
	c.peerImplicitTailcatReady = false
	c.tailcat.peerCapable = false
	c.tailcat.peerRequired = false
	c.tailcat.offerReceived = false
	c.transportSelectionReceived = false
	c.selectedDataTransport.Store(selectedTransportUnset)
	c.relayStandbyMu.Lock()
	c.relayStandbyReady = false
	c.relayStandbyMu.Unlock()
	c.tailcat.transferConnections.Store(0)
	c.tailcat.terminal.Store(false)
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
		log.Debugf("transfer attempt %d failed: %v", attempt, c.redactError(lastErr))
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

func normalizeReceiveFolder(folder string) (string, error) {
	cleanFolder, err := receivefs.Normalize(folder, true)
	if err != nil {
		return "", fmt.Errorf("filename must be a local path: %w", err)
	}
	if strings.Contains(cleanFolder, ".ssh") {
		return "", fmt.Errorf("invalid path detected: %q", folder)
	}
	return cleanFolder, nil
}

func normalizeReceiveFilePath(folder, name string) (string, string, error) {
	cleanFolder, err := normalizeReceiveFolder(folder)
	if err != nil {
		return "", "", err
	}
	cleanName, err := receivefs.Normalize(name, false)
	if err != nil || cleanName != path.Base(cleanName) {
		if err == nil {
			err = receivefs.ErrUnsafePath
		}
		return "", "", fmt.Errorf("filename must be a local path: %w", err)
	}
	destination := path.Clean(path.Join(cleanFolder, cleanName))
	if _, err = receivefs.Normalize(destination, false); err != nil {
		return "", "", fmt.Errorf("filename must be a local path: %w", err)
	}
	return cleanFolder, destination, nil
}

func validateReceiveSymlinkTarget(folder, target string) error {
	cleanTarget, err := receivefs.Normalize(target, false)
	if err != nil {
		return fmt.Errorf("symlink target must be a local path: %w", err)
	}
	if _, err = receivefs.Normalize(path.Join(folder, cleanTarget), false); err != nil {
		return fmt.Errorf("symlink target escapes receive directory: %w", err)
	}
	return nil
}

func validateReceiveMetadata(files []FileInfo, emptyFolders []FileInfo) ([]FileInfo, []FileInfo, error) {
	normalizedFiles := make([]FileInfo, len(files))
	normalizedEmptyFolders := make([]FileInfo, len(emptyFolders))
	entries := make([]receivefs.Entry, 0, len(files)+len(emptyFolders))

	for i, fi := range files {
		cleanFolder, destination, err := normalizeReceiveFilePath(fi.FolderRemote, fi.Name)
		if err != nil {
			return nil, nil, err
		}
		kind := receivefs.KindFile
		if fi.Symlink != "" {
			if err := validateReceiveSymlinkTarget(cleanFolder, fi.Symlink); err != nil {
				return nil, nil, err
			}
			kind = receivefs.KindSymlink
		}
		entries = append(entries, receivefs.Entry{Path: destination, Kind: kind})
		normalizedFiles[i] = fi
		normalizedFiles[i].FolderRemote = cleanFolder
		normalizedFiles[i].Name = path.Base(strings.ReplaceAll(fi.Name, "\\", "/"))
	}

	for i, fi := range emptyFolders {
		cleanFolder, err := normalizeReceiveFolder(fi.FolderRemote)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, receivefs.Entry{Path: cleanFolder, Kind: receivefs.KindDirectory})
		normalizedEmptyFolders[i] = fi
		normalizedEmptyFolders[i].FolderRemote = cleanFolder
	}
	if _, err := receivefs.ValidateEntries(entries); err != nil {
		return nil, nil, fmt.Errorf("duplicate destination path: %w", err)
	}

	return normalizedFiles, normalizedEmptyFolders, nil
}

func (c *Client) receiveFilesystem() (*receivefs.Root, error) {
	c.receiveRootMu.Lock()
	defer c.receiveRootMu.Unlock()
	if c.receiveRoot != nil {
		return c.receiveRoot, nil
	}
	root, err := receivefs.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	c.receiveRoot = root
	return root, nil
}

func (c *Client) closeReceiveFilesystem() {
	c.receiveRootMu.Lock()
	defer c.receiveRootMu.Unlock()
	if c.receiveRoot != nil {
		_ = c.receiveRoot.Close()
		c.receiveRoot = nil
	}
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
	c.sourceSnapshots = make([]os.FileInfo, len(filesInfo))
	totalFilesSize := int64(0)
	requestedAlgorithm := c.Options.HashAlgorithm
	if requestedAlgorithm == "" {
		requestedAlgorithm = "xxhash"
		c.Options.HashAlgorithm = requestedAlgorithm
	}
	c.preparedHashAlgorithm = requestedAlgorithm
	progressiveCandidate := requestedAlgorithm == "imohash"
	if progressiveCandidate {
		c.preparedHashAlgorithm = "imohash-v2"
	}
	preparedFirstRegular := false

	for i, fileInfo := range c.FilesToTransfer {
		fullPath := sourceFilePath(fileInfo)

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

		prepareNow := !progressiveCandidate || !fileInfo.Mode.IsRegular() || fileInfo.Size == 0 || !preparedFirstRegular
		if fileInfo.Mode.IsRegular() && fileInfo.Size > 0 && prepareNow {
			preparedFirstRegular = true
		}
		if prepareNow {
			if err = c.prepareFile(i, c.preparedHashAlgorithm); err != nil {
				return err
			}
		}
		totalFilesSize += fileInfo.Size
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

func sourceFilePath(fileInfo FileInfo) string {
	return filepath.Clean(fileInfo.FolderSource + string(os.PathSeparator) + fileInfo.Name)
}

func sourceInfoMatches(expected FileInfo, before, after os.FileInfo) bool {
	if before == nil || after == nil {
		return false
	}
	return expected.Size == before.Size() && expected.ModTime.Equal(before.ModTime()) &&
		expected.Mode == before.Mode() && before.Size() == after.Size() &&
		before.ModTime().Equal(after.ModTime()) && before.Mode() == after.Mode() &&
		os.SameFile(before, after)
}

func (c *Client) prepareFile(index int, algorithm string) error {
	if index < 0 || index >= len(c.FilesToTransfer) {
		return fmt.Errorf("invalid file preparation index %d", index)
	}
	fileInfo := c.FilesToTransfer[index]
	fullPath := sourceFilePath(fileInfo)
	before, err := os.Lstat(fullPath)
	if err != nil {
		return err
	}

	if !c.Options.NoCompress && fileInfo.Mode.IsRegular() && fileInfo.Size > 0 {
		c.FilesToTransfer[index].IsCompressed, _ = shouldCompressFile(
			fullPath,
			make([]byte, compressionSampleSize),
			nil,
		)
	}
	hash, err := c.stop.hash(fullPath, algorithm, fileInfo.Size > 1e7)
	if err != nil {
		return err
	}
	after, err := os.Lstat(fullPath)
	if err != nil {
		return err
	}
	if !sourceInfoMatches(fileInfo, before, after) {
		return fmt.Errorf("source changed while preparing %s", fullPath)
	}
	c.FilesToTransfer[index].Hash = hash
	c.FilesToTransfer[index].Prepared = true
	c.sourceSnapshots[index] = after
	log.Debugf("hashed %s to %x using %s", fullPath, hash, algorithm)
	return nil
}

func (c *Client) prepareAllFiles(algorithm string, force bool) error {
	for i := range c.FilesToTransfer {
		if !force && c.FilesToTransfer[i].Prepared {
			continue
		}
		if err := c.prepareFile(i, algorithm); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) finalizeHashNegotiation() error {
	requested := c.Options.HashAlgorithm
	if requested == "" {
		requested = "xxhash"
	}
	if c.preparedHashAlgorithm == "imohash-v2" && c.peerProgressiveHash {
		c.Options.HashAlgorithm = "imohash-v2"
		return nil
	}
	if c.preparedHashAlgorithm == "imohash-v2" {
		// Peers without progressive hash support, including v11.3 and current
		// browser receivers, only accept the eager xxhash wire format.
		c.Options.HashAlgorithm = "xxhash"
		c.preparedHashAlgorithm = "xxhash"
		return c.prepareAllFiles("xxhash", true)
	}
	c.Options.HashAlgorithm = requested
	return c.prepareAllFiles(requested, false)
}

type preparationFailure struct{ err error }

func (c *Client) startRemainingFilePreparation() {
	c.remainingPreparationOnce.Do(func() {
		go func() {
			for i := range c.FilesToTransfer {
				if c.FilesToTransfer[i].Prepared {
					continue
				}
				if err := c.prepareFile(i, c.preparedHashAlgorithm); err != nil {
					c.preparationErr.Store(preparationFailure{err: err})
					_ = message.Send(c.connection(0), c.Key, message.Message{Type: message.TypeError, Message: "source changed during file preparation"})
					c.stop.Cancel()
					return
				}
				prepared, err := json.Marshal(FilePrepared{
					Index:        i,
					Hash:         c.FilesToTransfer[i].Hash,
					IsCompressed: c.FilesToTransfer[i].IsCompressed,
				})
				if err != nil {
					c.preparationErr.Store(preparationFailure{err: err})
					c.stop.Cancel()
					return
				}
				if err = message.Send(c.connection(0), c.Key, message.Message{Type: message.TypeFilePrepared, Bytes: prepared}); err != nil {
					c.preparationErr.Store(preparationFailure{err: err})
					c.stop.Cancel()
					return
				}
			}
		}()
	})
}

func (c *Client) validateSourceUnchanged(index int) error {
	if index < 0 || index >= len(c.FilesToTransfer) || index >= len(c.sourceSnapshots) {
		return fmt.Errorf("invalid source index %d", index)
	}
	expected := c.sourceSnapshots[index]
	current, err := os.Lstat(sourceFilePath(c.FilesToTransfer[index]))
	if err != nil {
		return err
	}
	if expected == nil || !sourceInfoMatches(c.FilesToTransfer[index], expected, current) {
		return fmt.Errorf("source changed before transfer: %s", sourceFilePath(c.FilesToTransfer[index]))
	}
	return nil
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
	conn, banner, _, err := tcp.ConnectToTCPServer(localControlAddress, c.Options.RelayPassword, c.Options.RoomName)
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
			log.Debugf("received unexpected handshake payload (%d bytes)", len(data))
		}
	}
	c.setRelayControlRoute(localControlAddress, "")
	c.setConnection(0, conn)
	log.Debug("exchanged header message")
	c.Options.RelayPorts = strings.Split(banner, ",")
	if c.Options.NoMultiplexing {
		log.Debug("no multiplexing")
		c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
	}
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
		log.Tracef("received local-probe frame (%d bytes)", len(data))
		if kB != nil {
			var decryptErr error
			var dataDecrypt []byte
			dataDecrypt, decryptErr = decryptLocalProbePayload(kB, data)
			if decryptErr != nil {
				log.Tracef("error decrypting local-probe frame: %v", decryptErr)
				if strings.Contains(decryptErr.Error(), "message authentication failed") {
					return decryptErr
				}
			} else {
				data = dataDecrypt
				log.Tracef("decrypted local-probe frame (%d bytes)", len(data))
			}
		}
		if bytes.Equal(data, ipRequest) {
			if len(kB) == 0 {
				return errors.New("local IP request arrived before PAKE authentication")
			}
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
			log.Tracef("sending %d encrypted local endpoint candidates", len(ips))
			bips, err := json.Marshal(ips)
			if err != nil {
				log.Tracef("error marshalling ips: %v", err)
			}
			bips, err = encryptLocalProbePayload(kB, bips)
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
			log.Tracef("[%+v] got unexpected local-probe frame (%d bytes)", conn, len(data))
			return fmt.Errorf("gracefully refusing using the public relay")
		}
	}
}

// receiveControlFrame ignores relay keepalives while a pre-transfer control
// exchange is waiting for its next application frame. A keepalive may be
// queued immediately after room admission, before the peer's PAKE response.
func receiveControlFrame(conn *comm.Comm) ([]byte, error) {
	deadline := time.Now().Add(localProbeResponseTimeout)
	for {
		data, err := conn.ReceiveWithDeadline(deadline)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(data, []byte{1}) {
			log.Trace("got ping")
			continue
		}
		return data, nil
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
		conn, banner, ipaddr, capability, err := tcp.ConnectToTCPServerControl(address, c.Options.RelayPassword, room)
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
		c.setRelayControlRoute(address, capability)
		c.setConnection(0, conn)
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
	c.setReceiveStatus(receiveStatusConnecting)
	if err := c.reconnectRelayAttempt(func(conn *comm.Comm) error {
		return conn.Send(handshakeRequest)
	}); err != nil {
		return err
	}
	log.Debug("exchanged reconnect header message")
	c.setReceiveStatus(receiveStatusWaitingForSender)
	return nil
}

// Send will send the specified file
func (c *Client) Send(filesInfo []FileInfo, emptyFoldersToTransfer []FileInfo, totalNumberFolders int) (err error) {
	defer func() { err = c.redactError(err) }()
	go c.stop.done()
	defer c.stop.Cancel()
	defer c.closeTailcatBundle()
	c.EmptyFoldersToTransfer = emptyFoldersToTransfer
	c.TotalNumberFolders = totalNumberFolders
	c.TotalNumberOfContents = len(filesInfo)
	c.FilesToTransfer = filesInfo
	c.tailcat.transferBytes.Store(totalLogicalTransferSize(filesInfo))
	c.startTailcatPreparation()
	hashResult := make(chan error, 1)
	go func() {
		prepareErr := c.sendCollectFiles(filesInfo)
		c.finishFilePreparation(prepareErr)
		hashResult <- prepareErr
	}()
	flags := &strings.Builder{}
	if !c.Options.PublicRelay && c.Options.RelayAddress != models.DEFAULT_RELAY && !c.Options.OnlyLocal {
		flags.WriteString("--relay " + c.Options.RelayAddress + " ")
	}
	if c.Options.RelayPassword != models.DEFAULT_PASSPHRASE {
		flags.WriteString("--pass " + c.Options.RelayPassword + " ")
	}
	webURL := ""
	if c.Options.Transport != TransportDERP {
		webURL = webReceiveURL(c.Options.SharedSecret)
	}
	clipboardNotice := ""
	if !c.Options.DisableClipboard {
		clipboardText := formatClipboardText(c.Options.SharedSecret, flags.String(), c.Options.ExtendedClipboard)
		if copyToClipboard(clipboardText, true, c.Options.ExtendedClipboard) {
			clipboardNotice = "code copied to clipboard"
			if c.Options.ExtendedClipboard {
				clipboardNotice = "command copied to clipboard"
			}
		}
	}
	output, colorEnabled := termui.Output(os.Stderr)
	fmt.Fprint(output, formatSendInstructions(c.Options.SharedSecret, flags.String(), webURL, clipboardNotice, colorEnabled))
	if c.Options.ShowQrCode && webURL != "" {
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
			defer c.markExternalIPReady()
			route, routeErr := c.connectRelayControl(c.Options.RelayAddress6, c.Options.RelayAddress)
			if routeErr != nil {
				routeErr = fmt.Errorf("%w: could not connect to %s: %v", ErrRelayConnection, c.Options.RelayAddress, routeErr)
				log.Debug(routeErr)
				errchan <- routeErr
				return
			}
			log.Debugf("banner: %s", route.banner)
			log.Debugf("connection established: %+v", route.connection)
			// Preserve the public relay's observation before a direct route can
			// switch the sender to its own loopback relay.
			c.ExternalIP = route.externalIP
			c.markExternalIPReady()
			if routeErr = c.senderWaitForHandshake(route.connection); routeErr != nil {
				errchan <- routeErr
				return
			}

			c.setRelayControlRoute(route.address, route.capability)
			c.setConnection(0, route.connection)
			c.Options.RelayPorts = strings.Split(route.banner, ",")
			if c.Options.NoMultiplexing {
				log.Debug("no multiplexing")
				c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
			}
			log.Debug("exchanged header message")
			c.markSenderRouteReady()
			errchan <- c.transferWithReconnect(func(attempt int) error {
				if attempt == 0 {
					return nil
				}
				return c.senderReconnectRelayAttempt(attempt)
			})
		}()
	} else {
		c.markExternalIPReady()
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
		log.Debugf("error from errchan: %v", c.redactError(err))
		if strings.Contains(err.Error(), "could not secure channel") {
			return err
		}
	}
	if !c.Options.DisableLocal {
		if isFatalSenderRouteError(err) {
			return err
		}
		log.Debugf("waiting for alternate sender route after: %v", c.redactError(err))
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
	c.setReceiveStatus(receiveStatusLookingForSender)
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
	defer func() { err = c.redactError(err) }()
	go c.stop.done()
	defer c.stop.Cancel()
	defer c.closeTailcatBundle()
	defer c.clearReceiveStatus()
	if _, err = c.receiveFilesystem(); err != nil {
		return err
	}
	defer c.closeReceiveFilesystem()
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

	c.setReceiveStatus(receiveStatusConnecting)
	var route relayControlResult
	if !c.Options.DisableLocal && !isIPset {
		log.Debug("racing peer discovery with the public relay")
		route, usingLocal, err = c.connectReceiverRelayControl(c.Options.RelayAddress6, c.Options.RelayAddress)
	} else {
		route, err = c.connectRelayControl(c.Options.RelayAddress6, c.Options.RelayAddress)
	}
	if err != nil {
		err = fmt.Errorf("could not connect to %s: %w", c.Options.RelayAddress, err)
		log.Debug(err)
		return
	}
	c.ExternalIP = route.externalIP
	if usingLocal {
		c.ExternalIPConnected = route.address
	}
	c.setConnection(0, route.connection)
	c.setRelayControlRoute(route.address, route.capability)
	log.Debugf("receiver connection established: %+v", c.connection(0))
	log.Debugf("banner: %s", route.banner)
	banner := route.banner

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
			if err = c.connection(0).Send(data); err != nil {
				log.Errorf("dataMessage send error: %v", err)
				return
			}
			data, err = receiveControlFrame(c.connection(0))
			if err != nil {
				return
			}
			err = json.Unmarshal(data, &dataMessage)
			if err != nil || dataMessage.Kind != "pake2" {
				log.Debugf("received invalid local PAKE response (%d bytes)", len(data))
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
			data, err = encryptLocalProbePayload(kA, ipRequest)
			if err != nil {
				return
			}
			log.Debug("sending ips?")
			if err = c.connection(0).Send(data); err != nil {
				log.Errorf("ips send error: %v", err)
			}
			data, err = receiveControlFrame(c.connection(0))
			if err != nil {
				return
			}
			data, err = decryptLocalProbePayload(kA, data)
			if err != nil {
				return
			}
			log.Debugf("received encrypted local endpoint list (%d bytes)", len(data))
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
					log.Debug(c.redactError(errConn))
					log.Debug("could not connect to " + serverTry)
					continue
				}
				log.Debugf("local connection established to %s", serverTry)
				log.Debugf("banner: %s", banner2)
				// reset to the local port
				banner = banner2
				c.setRelayControlRoute(serverTry, "")
				c.ExternalIPConnected = peerIP(serverTry)
				c.ExternalIP = externalIP
				c.connection(0).Close()
				c.setConnection(0, conn)
				break
			}
		}
	}

	if err = c.connection(0).Send(handshakeRequest); err != nil {
		log.Errorf("handshake send error: %v", err)
	}
	c.Options.RelayPorts = strings.Split(banner, ",")
	if c.Options.NoMultiplexing {
		log.Debug("no multiplexing")
		c.Options.RelayPorts = []string{c.Options.RelayPorts[0]}
	}
	log.Debug("exchanged header message")
	c.setReceiveStatus(receiveStatusWaitingForSender)
	err = c.transferWithReconnect(func(attempt int) error {
		if attempt == 0 {
			return nil
		}
		return c.receiverReconnectRelayAttempt(attempt)
	})
	if err == nil {
		if c.numberOfTransferredFiles+len(c.EmptyFoldersToTransfer) == 0 {
			output, colorEnabled := termui.Output(os.Stderr)
			fmt.Fprint(output, formatNoTransferSummary(c.FilesToTransfer, c.numberOfUnchangedFiles, colorEnabled))
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
		control: c.connection(0),
		tailcat: tailcatAttemptState{setupDone: make(chan struct{})},
	}
	defer attempt.closeTailcatPending()
	defer attempt.cancelTailcatSetup()

	// if recipient, initialize with sending pake information
	log.Debug("ready")
	if !c.Options.IsSender && !c.lifecycleSnapshot().ChannelSecured {
		c.pakeInitiator = append([]byte(nil), c.Pake.Bytes()...)
		c.pakeCurve = c.Options.Curve
		err = message.Send(c.connection(0), c.Key, message.Message{
			Type:     message.TypePAKE,
			Version:  pakekey.ProtocolVersion,
			Bytes:    c.pakeInitiator,
			Bytes2:   []byte(c.Options.Curve),
			Features: c.pakeFeatures(),
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
		data, err = c.connection(0).Receive()
		if err != nil {
			log.Debugf("got error receiving: %v", c.redactError(err))
			select {
			case reportedErr := <-attempt.errc:
				err = reportedErr
			default:
				if !c.lifecycleSnapshot().ChannelSecured {
					err = fmt.Errorf("could not secure channel")
				} else if c.activeTransferStarted() {
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
			log.Debugf("failed to process transfer frame (%d bytes)", len(data))
			log.Debugf("got error processing: %v", c.redactError(err))
			break
		}
		if done {
			break
		}
	}
	state := c.lifecycleSnapshot()
	if err := c.ctxErr(); err != nil && state.Successful {
		c.updateLifecycle(func(state *transferLifecycle) { state.Successful = false })
		state.Successful = false
		log.Tracef("SuccessfulTransfer: %v", c.redactError(err))
	}
	// purge errors that come from successful transfer
	if state.Successful {
		if err != nil {
			log.Debugf("purging error: %s", c.redactError(err))
		}
		err = nil
	}
	if c.Options.IsSender && state.Successful {
		for _, file := range c.FilesToTransfer {
			if file.TempFile {
				fmt.Println("Removing " + file.Name)
				os.Remove(file.Name)
			}
		}
	}

	if state.Successful && !c.Options.IsSender {
		if extractErr := c.extractReceivedArchives(); extractErr != nil {
			c.updateLifecycle(func(state *transferLifecycle) { state.Successful = false })
			err = extractErr
			log.Error(err)
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
		root, rootErr := c.receiveFilesystem()
		if rootErr != nil {
			return rootErr
		}
		if err = root.Remove(pathToFile); err != nil {
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

// extractReceivedArchives treats TempFile only as an archive-candidate hint.
// The exact received file is opened through the receive root, and a complete
// ZIP manifest and size validation must succeed before any member is committed.
// The received archive remains recoverable if validation or extraction fails.
func (c *Client) extractReceivedArchives() error {
	root, err := c.receiveFilesystem()
	if err != nil {
		return err
	}
	for _, file := range c.FilesToTransfer {
		if !file.TempFile {
			continue
		}
		_, archivePath, pathErr := normalizeReceiveFilePath(file.FolderRemote, file.Name)
		if pathErr != nil {
			return errors.New("received archive failed validation or extraction")
		}
		archive, openErr := root.Open(archivePath)
		if openErr != nil {
			return errors.New("received archive failed validation or extraction")
		}
		archiveInfo, statErr := archive.Stat()
		if statErr == nil {
			statErr = utils.UnzipDirectoryFromFileAtRootWithLimit(root, archive, archiveInfo.Size())
		}
		closeErr := archive.Close()
		if statErr != nil || closeErr != nil {
			return errors.New("received archive failed validation or extraction")
		}
		if removeErr := root.Remove(archivePath); removeErr != nil {
			return fmt.Errorf("remove validated received archive: %w", removeErr)
		}
		log.Debug("removed validated received archive")
	}
	return nil
}

func (c *Client) createEmptyFolder(i int) (err error) {
	folderRemote, err := normalizeReceiveFolder(c.EmptyFoldersToTransfer[i].FolderRemote)
	if err != nil {
		return
	}
	c.EmptyFoldersToTransfer[i].FolderRemote = folderRemote
	root, err := c.receiveFilesystem()
	if err != nil {
		return err
	}
	err = root.MkdirAll(c.EmptyFoldersToTransfer[i].FolderRemote, os.ModePerm)
	if err != nil {
		return
	}
	output, colorEnabled := termui.Output(os.Stderr)
	fmt.Fprintln(output, termui.Filename(c.EmptyFoldersToTransfer[i].FolderRemote, colorEnabled))
	c.setProgressBar(c.newProgressBar(1, " ", 0))
	c.finishProgress()
	return
}

func (c *Client) processMessageFileInfo(m message.Message) (done bool, err error) {
	c.clearReceiveStatus()
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
	if c.Options.HashAlgorithm == "imohash-v2" && !c.peerProgressiveHash {
		return true, errors.New("peer sent imohash-v2 without negotiating progressive-file-hash-v1")
	}
	c.peerReconnectVersion = senderInfo.ReconnectVersion
	if c.peerInlineMetadata {
		c.ExternalIPConnected = preferredPeerIP(c.ExternalIPConnected, senderInfo.ExternalIP)
	}
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
		choicePrompt := termui.PromptChoices("(Y/n)", colorEnabled)
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
			err = message.Send(c.connection(0), c.Key, message.Message{
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
	fmt.Fprintf(output, "\nReceiving (<-%s)\n", peerIP(c.ExternalIPConnected))

	for i := 0; i < len(c.EmptyFoldersToTransfer); i += 1 {
		root, rootErr := c.receiveFilesystem()
		if rootErr != nil {
			return false, rootErr
		}
		_, errExists := root.Stat(c.EmptyFoldersToTransfer[i].FolderRemote)
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
					termui.PromptChoices("(y/N)", colorEnabled),
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
		c.updateLifecycle(func(state *transferLifecycle) {
			state.Successful = true
			state.RecipientRequested = true
			state.FileTransferred = true
		})
		c.tailcat.terminal.Store(true)
		c.markTransferStarted()
		errStopTransfer := message.Send(c.connection(0), c.Key, message.Message{
			Type: message.TypeFinished,
		})
		if errStopTransfer != nil {
			err = errStopTransfer
		}
	}
	log.Debug(c.FilesToTransfer)
	c.updateLifecycle(func(state *transferLifecycle) { state.FileInfoTransferred = true })
	c.markStartup("file-metadata-ready")
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
	if !c.Options.IsSender {
		c.setReceiveStatus(receiveStatusAuthenticatingCode)
	}
	if m.Version != pakekey.ProtocolVersion {
		return incompatiblePakeVersionError{got: m.Version}
	}
	c.tailcat.peerCapable = supportsFeature(m.Features, tailcatFeature)
	c.tailcat.peerRequired = supportsFeature(m.Features, tailcatRequiredFeature)
	c.peerInlineMetadata = supportsFeature(m.Features, inlinePeerMetadataFeature)
	c.peerProgressiveHash = supportsFeature(m.Features, progressiveFileHashFeature)
	c.peerStagedTransport = supportsFeature(m.Features, stagedTransportFeature)
	c.peerImplicitTailcatReady = supportsFeature(m.Features, implicitTailcatReadyFeature)
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
		err = message.Send(c.connection(0), nil, message.Message{
			Type:     message.TypePAKE,
			Version:  pakekey.ProtocolVersion,
			Bytes:    c.pakeResponder,
			Bytes2:   salt,
			Features: c.pakeFeatures(),
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
		err = message.Send(c.connection(0), nil, message.Message{
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
		if err := message.Send(c.connection(0), nil, message.Message{
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
	c.markStartup("peer-pake-complete")
	return c.activateSecureChannel(attempt)
}

func (c *Client) activateRelayDataChannels(attempt *transferAttemptState) (err error) {
	limit := min(len(c.Options.RelayPorts), 8)
	indices := make([]int, limit)
	for i := range indices {
		indices[i] = i
	}
	if err := c.openRelayDataChannels(attempt, indices, !c.Options.IsSender); err != nil {
		return err
	}
	c.selectedDataTransport.Store(selectedTransportRelay)
	attempt.finishTailcatSetup()
	return c.finishDataTransportActivation()
}

func (c *Client) openRelayDataChannels(attempt *transferAttemptState, indices []int, startReceive bool) (err error) {
	var wg sync.WaitGroup
	relayControlAddress, relayCapability := c.currentRelayControlRoute()
	if relayControlAddress == "" {
		relayControlAddress = c.Options.RelayAddress
	}
	relayControlAddress = normalizeRelayAddress(relayControlAddress)
	relayHost, _, err := net.SplitHostPort(relayControlAddress)
	if err != nil {
		return fmt.Errorf("bad relay address %s: %w", relayControlAddress, err)
	}

	errc := make(chan error, len(indices))
	wg.Add(len(indices))
	for _, index := range indices {
		if index < 0 || index >= len(c.Options.RelayPorts) || index >= 8 {
			wg.Done()
			errc <- fmt.Errorf("invalid relay data index %d", index)
			continue
		}
		log.Debugf("port: [%s]", c.Options.RelayPorts[index])
		go func(j int) {
			defer wg.Done()
			server := net.JoinHostPort(relayHost, c.Options.RelayPorts[j])
			log.Debugf("connecting to %s", server)
			dataConn, _, _, fast, connErr := tcp.ConnectToTCPServerWithCapability(
				server,
				c.Options.RelayPassword,
				fmt.Sprintf("%s-%d", c.Options.RoomName, j),
				relayCapability,
			)
			if connErr != nil {
				errc <- connErr
				return
			}
			if !c.installRelayDataConnection(j+1, dataConn) {
				return
			}
			log.Debugf("connected to %s", server)
			if fast {
				log.Debugf("used fast relay admission on data port %d", j)
			}
			if startReceive && !c.Options.IsSender {
				go c.receiveData(j, dataConn, attempt)
			} else if c.Options.IsSender {
				c.startLateSenderWorker(dataConn)
			}
		}(index)
	}
	wg.Wait()
	close(errc)
	for connectErr := range errc {
		if connectErr != nil {
			return fmt.Errorf("%w: could not connect transfer ports: %v", ErrRelayConnection, connectErr)
		}
	}
	return nil
}

type tailcatListenResult struct {
	listener tailcatDataListener
	err      error
}

type tailcatDialResult struct {
	bundle *tailcatDataBundle
	err    error
}

func (c *Client) advertisedExternalIP() string {
	localIPs, _ := utils.GetLocalIPs()
	return preferredPublicIP(c.ExternalIP, localIPs)
}

func (c *Client) sendExternalIP() error {
	log.Debug("sending external IP")
	return message.Send(c.connection(0), c.Key, message.Message{
		Type:    message.TypeExternalIP,
		Message: c.advertisedExternalIP(),
		Bytes:   c.pakeResponder,
	})
}

func (c *Client) finishDataTransportActivation() error {
	if !c.peerInlineMetadata {
		if !c.Options.IsSender {
			return c.sendExternalIP()
		}
		return nil
	}
	log.Debug("peer endpoint metadata will use existing transfer messages")
	c.updateLifecycle(func(state *transferLifecycle) { state.ChannelSecured = true })
	c.markStartup("transport-ready")
	if !c.Options.IsSender {
		c.setReceiveStatus(receiveStatusWaitingForFileList)
	}
	return nil
}

func (c *Client) processExternalIP(m message.Message) (done bool, err error) {
	log.Debug("received encrypted external endpoint metadata")
	if c.Options.IsSender {
		c.waitForExternalIP()
		localIPs, _ := utils.GetLocalIPs()
		advertisedIP := preferredPublicIP(c.ExternalIP, localIPs)
		log.Debugf("advertising public IP: %s", advertisedIP)
		err = message.Send(c.connection(0), c.Key, message.Message{
			Type:    message.TypeExternalIP,
			Message: advertisedIP,
		})
		if err != nil {
			return true, err
		}
	}
	c.ExternalIPConnected = preferredPeerIP(c.ExternalIPConnected, m.Message)
	log.Debug("peer endpoint metadata exchange completed")
	c.updateLifecycle(func(state *transferLifecycle) { state.ChannelSecured = true })
	c.markStartup("transport-ready")
	if !c.Options.IsSender {
		c.setReceiveStatus(receiveStatusWaitingForFileList)
	}
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
		err = message.Send(c.connection(0), c.Key, message.Message{
			Type: message.TypeFinished,
		})
		done = true
		c.updateLifecycle(func(state *transferLifecycle) { state.Successful = true })
		c.tailcat.terminal.Store(true)
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
	case message.TypeTailcatOffer:
		err = c.processTailcatOffer(m, attempt)
	case message.TypeTailcatStatus:
		err = c.processUnexpectedTailcatStatus(m)
	case message.TypeTransportSelect:
		err = c.processTransportSelect(m, attempt)
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
	case message.TypeFilePrepared:
		err = c.processMessageFilePrepared(m)
	case message.TypeExactHashRequest:
		err = c.processExactHashRequest(m)
	case message.TypeExactHashResult:
		err = c.processExactHashResult(m)
	case message.TypeRelayStandby:
		err = c.processRelayStandby(attempt)
	case message.TypeRelayRamp:
		err = c.processRelayRamp(attempt)
	case message.TypeRecipientReady:
		var remoteFile RemoteFileRequest
		err = json.Unmarshal(m.Bytes, &remoteFile)
		if err != nil {
			return
		}
		c.peerReconnectVersion = remoteFile.ReconnectVersion
		c.peerPerFileCompression = supportsFeature(remoteFile.Features, perFileCompressionFeature)
		if c.peerInlineMetadata {
			c.ExternalIPConnected = preferredPeerIP(c.ExternalIPConnected, remoteFile.ExternalIP)
		}
		c.FilesToTransferCurrentNum = remoteFile.FilesToTransferCurrentNum
		c.CurrentFileChunkRanges = remoteFile.CurrentFileChunkRanges
		c.CurrentFileChunkCount = utils.ChunkRangesCount(
			c.CurrentFileChunkRanges,
			c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
			models.TCP_BUFFER_SIZE/2,
		)
		log.Debugf("current file has %d requested chunks", c.CurrentFileChunkCount)
		c.updateLifecycle(func(state *transferLifecycle) { state.RecipientRequested = true })
		c.markTransferStarted()

		if c.Options.Ask {
			output, colorEnabled := termui.Output(os.Stderr)
			fmt.Fprintf(output, "Send to machine '%s'? %s ",
				remoteFile.MachineID,
				termui.PromptChoices("(Y/n)", colorEnabled),
			)
			choice, errInput := utils.GetInput("")
			choice = strings.ToLower(choice)
			if errInput != nil || (choice != "" && choice != "y" && choice != "yes") {
				err = message.Send(c.connection(0), c.Key, message.Message{
					Type:    message.TypeError,
					Message: "refusing files",
				})
				done = true
				return
			}
		}
	case message.TypeCloseSender:
		c.finishProgress()
		log.Debug("close-sender received...")
		c.updateLifecycle(func(state *transferLifecycle) {
			state.FileTransferred = false
			state.RecipientRequested = false
		})
		log.Debug("sending close-recipient")
		err = message.Send(c.connection(0), c.Key, message.Message{
			Type: message.TypeCloseRecipient,
		})
	case message.TypeCloseRecipient:
		c.updateLifecycle(func(state *transferLifecycle) {
			state.FileTransferred = false
			state.RecipientRequested = false
		})
	}
	if err != nil {
		log.Debugf("got error from processing message: %v", c.redactError(err))
		return
	}
	err = c.updateState(attempt)
	if err != nil {
		log.Debugf("got error from updating state: %v", c.redactError(err))
		return
	}
	return
}

func (c *Client) updateIfSenderChannelSecured() (err error) {
	state := c.lifecycleSnapshot()
	if c.Options.IsSender && state.ChannelSecured && !state.FileInfoTransferred {
		if err := c.waitForFilesReady(c.stop.ctx); err != nil {
			return err
		}
		if failure, ok := c.preparationErr.Load().(preparationFailure); ok {
			return failure.err
		}
		if err := c.finalizeHashNegotiation(); err != nil {
			return err
		}
		var b []byte
		machID, _ := machineid.ID()
		nextReconnectRoom := ""
		externalIP := ""
		if c.peerInlineMetadata {
			externalIP = c.advertisedExternalIP()
		}
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
			ExternalIP:             externalIP,
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
		err = message.Send(c.connection(0), c.Key, message.Message{
			Type:  message.TypeFileInfo,
			Bytes: b,
		})
		if err != nil {
			return
		}

		c.updateLifecycle(func(state *transferLifecycle) { state.FileInfoTransferred = true })
		if c.peerProgressiveHash && c.Options.HashAlgorithm == "imohash-v2" {
			c.startRemainingFilePreparation()
		}
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
	root, err := c.receiveFilesystem()
	if err != nil {
		return err
	}
	if folderForFileBase != "." && folderForFileBase != "" {
		if err := root.MkdirAll(folderForFile, os.ModePerm); err != nil {
			log.Errorf("can't create %s: %v", folderForFile, err)
			return err
		}
	}
	var errOpen error
	c.CurrentFile, errOpen = root.OpenFile(
		pathToFile,
		os.O_RDWR, 0o666)
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
		c.CurrentFile, errOpen = root.OpenFile(pathToFile, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if errOpen != nil {
			errOpen = fmt.Errorf("could not create %s: %w", pathToFile, errOpen)
			log.Error(errOpen)
			return errOpen
		}
		errChmod := c.CurrentFile.Chmod(c.FilesToTransfer[c.FilesToTransferCurrentNum].Mode.Perm())
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
		err = message.Send(c.connection(0), c.Key, message.Message{
			Type: message.TypeFinished,
		})
		if err != nil {
			return
		}
		c.updateLifecycle(func(state *transferLifecycle) { state.Successful = true })
		c.tailcat.terminal.Store(true)
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
	externalIP := ""
	if c.peerInlineMetadata {
		externalIP = c.advertisedExternalIP()
	}
	bRequest, _ := json.Marshal(RemoteFileRequest{
		CurrentFileChunkRanges:    c.CurrentFileChunkRanges,
		FilesToTransferCurrentNum: c.FilesToTransferCurrentNum,
		MachineID:                 machID,
		ExternalIP:                externalIP,
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
	c.markStartup("recipient-ready")
	err = message.Send(c.connection(0), c.Key, message.Message{
		Type:  message.TypeRecipientReady,
		Bytes: bRequest,
	})
	if err != nil {
		return
	}
	c.updateLifecycle(func(state *transferLifecycle) { state.RecipientRequested = true })
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

	maxDescription := max(width-progressMetaWidth, minDescription)

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
	root, err := c.receiveFilesystem()
	if err != nil {
		return err
	}
	if err = root.MkdirAll(fileInfo.FolderRemote, os.ModePerm); err != nil {
		log.Error(err)
		return err
	}
	if fileInfo.Symlink != "" {
		if err = validateReceiveSymlinkTarget(fileInfo.FolderRemote, fileInfo.Symlink); err != nil {
			return
		}
		log.Debug("creating symlink")
		// remove symlink if it exists
		if _, errExists := root.Lstat(pathToFile); errExists == nil {
			if err = root.Remove(pathToFile); err != nil {
				return err
			}
		}
		err = root.Symlink(fileInfo.Symlink, pathToFile)
		if err != nil {
			return
		}
	} else {
		emptyFile, errCreate := root.OpenFile(pathToFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
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
	c.setProgressBar(c.newProgressBar(1, formatDescription(description), 0))
	c.finishProgress()
	return
}

func (c *Client) updateIfRecipientHasFileInfo() (err error) {
	state := c.lifecycleSnapshot()
	if c.Options.IsSender || !state.FileInfoTransferred || state.RecipientRequested {
		return
	}
	// find the next file to transfer and send that number
	// if the files are the same size, then look for missing chunks
	finished := true
	root, err := c.receiveFilesystem()
	if err != nil {
		return err
	}
	for i, fileInfo := range c.FilesToTransfer {
		if _, ok := c.FilesHasFinished[i]; ok {
			continue
		}
		if i < c.FilesToTransferCurrentNum {
			continue
		}
		if c.Options.HashAlgorithm == "imohash-v2" && !fileInfo.Prepared {
			log.Debugf("waiting for file %d preparation", i)
			return nil
		}
		log.Debugf("checking %+v", fileInfo)
		recipientFileInfo, errRecipientFile := root.Lstat(path.Join(fileInfo.FolderRemote, fileInfo.Name))
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
		hashesEqual := bytes.Equal(fileHash, fileInfo.Hash)
		if hashesEqual && c.Options.HashAlgorithm == "imohash-v2" && errRecipientFile == nil {
			known, exactMatch, pending := c.exactHashDecision(i)
			switch {
			case known:
				hashesEqual = exactMatch
				if !exactMatch {
					fileHash = nil
				}
			case pending:
				return nil
			default:
				if err := c.beginExactHash(i, path.Join(fileInfo.FolderRemote, fileInfo.Name)); err != nil {
					return err
				}
				return nil
			}
		}
		log.Debugf("%s %+x %+x %+v", fileInfo.Name, fileHash, fileInfo.Hash, errHash)
		if !hashesEqual {
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
				styledChoice := termui.PromptChoices("(y/N)", colorEnabled)
				if action == "Resume" {
					styledAction = action
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
			c.numberOfUnchangedFiles++

			if !fileInfo.ModTime.IsZero() {
				if err := root.Chtimes(path.Join(fileInfo.FolderRemote, fileInfo.Name), fileInfo.ModTime, fileInfo.ModTime); err != nil {
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
		if errHash != nil || !hashesEqual {
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

	state := c.lifecycleSnapshot()
	if c.Options.IsSender && state.RecipientRequested && !state.FileTransferred {
		log.Debug("start sending data!")

		if !c.firstSend {
			output, _ := termui.Output(os.Stderr)
			fmt.Fprintf(output, "\nSending (->%s)\n", peerIP(c.ExternalIPConnected))
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

					c.setProgressBar(c.newProgressBar(1, formatDescription(description), 0))
					c.finishProgress()
				}
			}
		}
		c.updateLifecycle(func(state *transferLifecycle) { state.FileTransferred = true })
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
		if err = c.validateSourceUnchanged(c.FilesToTransferCurrentNum); err != nil {
			return err
		}
		c.fread, err = os.Open(pathToFile)
		c.numfinished = 0
		if err != nil {
			return
		}
		c.startSenderChunkQueue(attempt, c.fread)
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
	c.setProgressBar(c.newProgressBar(
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		formatDescription(description),
		100*time.Millisecond,
	))
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
			c.addProgress(bytesDone)
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
			if c.selectedDataTransport.Load() == selectedTransportTailcat && c.tailcat.terminal.Load() && tailcattransport.IsExpectedClose(err) {
				return
			}
			if c.ctxErr() == nil {
				if c.activeTransferStarted() {
					attempt.report(transferDisconnectError{err: err})
				} else if c.selectedDataTransport.Load() == selectedTransportTailcat {
					attempt.report(c.tailcatError("data connection", err, ""))
				}
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
		c.markStartup("first-decrypted-data-byte")
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
			if sendErr := message.Send(c.connection(0), c.Key, message.Message{
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

		c.addProgress(int64(len(data[8:])))
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
				root, rootErr := c.receiveFilesystem()
				if rootErr != nil {
					attempt.report(rootErr)
					return
				}
				file, openErr := root.Open(pathToFile)
				if openErr != nil {
					attempt.report(openErr)
					return
				}
				b, readErr := io.ReadAll(file)
				closeErr := file.Close()
				if readErr != nil {
					attempt.report(readErr)
					return
				}
				if closeErr != nil {
					attempt.report(closeErr)
					return
				}
				fmt.Print(string(b))
			}
			log.Debug("sending close-sender")
			err = message.Send(c.connection(0), c.Key, message.Message{
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

func (c *Client) startSenderChunkQueue(attempt *transferAttemptState, file *os.File) {
	queue := newRequestedChunkQueue(
		c.CurrentFileChunkRanges,
		c.FilesToTransfer[c.FilesToTransferCurrentNum].Size,
		models.TCP_BUFFER_SIZE/2,
		func() {
			log.Debug("closing file")
			if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				log.Errorf("error closing file: %v", err)
			}
		},
	)
	c.senderDataMu.Lock()
	c.senderChunkQueue = queue
	c.senderDataAttempt = attempt
	c.senderDataFile = file
	c.senderDataWorkers = make(map[*comm.Comm]struct{})
	c.senderWorkerSequence = 0
	c.senderDataMu.Unlock()
	connections := c.connectionsSnapshot()
	if len(connections) > 0 {
		connections = connections[1:]
	}
	for i, connection := range connections {
		if connection != nil {
			log.Debugf("starting sending over comm %d", i)
			c.startLateSenderWorker(connection)
		}
	}
}

func (c *Client) startLateSenderWorker(dataConn *comm.Comm) {
	if dataConn == nil {
		return
	}
	c.senderDataMu.Lock()
	queue, file, attempt := c.senderChunkQueue, c.senderDataFile, c.senderDataAttempt
	if queue == nil || file == nil || attempt == nil {
		c.senderDataMu.Unlock()
		return
	}
	if _, exists := c.senderDataWorkers[dataConn]; exists {
		c.senderDataMu.Unlock()
		return
	}
	c.senderDataWorkers[dataConn] = struct{}{}
	workerID := c.senderWorkerSequence
	c.senderWorkerSequence++
	c.senderDataMu.Unlock()
	go c.sendData(workerID, dataConn, file, queue, attempt)
}

func (c *Client) sendData(i int, dataConn *comm.Comm, fread *os.File, queue *requestedChunkQueue, attempt *transferAttemptState) {
	defer func() {
		if r := recover(); r != nil {
			attempt.report(fmt.Errorf("send data panic: %v", r))
		}
		log.Debugf("finished with %d", i)
	}()

	chunkSize := int64(models.TCP_BUFFER_SIZE / 2)
	payload := make([]byte, 8+chunkSize)
	var encryptedBuffer []byte
	var compressedBuffer []byte
	for {
		if err := c.ctxErr(); err != nil {
			log.Tracef("stopping send %d: %v", i, err)
			return
		}
		readingPos, ok := queue.claim()
		if !ok {
			return
		}

		n, errRead := fread.ReadAt(payload[8:], readingPos)
		if n == 0 {
			if errRead == nil {
				errRead = io.ErrUnexpectedEOF
			}
			attempt.report(errRead)
			return
		}
		if c.limiter != nil {
			r := c.limiter.ReserveN(time.Now(), n)
			log.Debugf("Limiting Upload for %d", r.Delay())
			time.Sleep(r.Delay())
		}
		if n > 0 {
			binary.LittleEndian.PutUint64(payload[:8], uint64(readingPos))
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
			c.addProgress(int64(n))
			c.mutex.Lock()
			c.TotalSent += int64(n)
			c.mutex.Unlock()
			queue.complete()
		}

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
func copyToClipboard(str string, quiet bool, extendedClipboard bool) bool {
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
		return false
	}
	// Nothing has been found
	if cmd == nil {
		return false
	}
	// Sending stdin into the available clipboard program
	cmd.Stdin = bytes.NewReader([]byte(str))
	if err := cmd.Run(); err != nil {
		log.Debugf("error copying to clipboard: %v", err)
		return false
	}
	if !quiet {
		output, colorEnabled := termui.Output(os.Stderr)
		if extendedClipboard {
			fmt.Fprintln(output, termui.Success("Command copied to clipboard!", colorEnabled))
		} else {
			fmt.Fprintln(output, termui.Success("Code copied to clipboard!", colorEnabled))
		}
	}
	return true
}

// CopyToClipboard copies a croc share value using the platform clipboard helper.
func CopyToClipboard(value string, quiet bool, extended bool) {
	copyToClipboard(value, quiet, extended)
}
