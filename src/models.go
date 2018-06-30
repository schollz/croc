package croc

import (
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/schollz/croc/src/pake"
)

const (
	// maximum buffer size for initial TCP communication
	bufferSize = 1024
)

type Croc struct {
	// Options for connecting to server
	TcpPorts         []string
	WebsocketAddress string
	ServerPort       string
	Timeout          time.Duration

	// Options for file transfering
	UseEncryption       bool
	UseCompression      bool
	CurveType           string
	AllowLocalDiscovery bool

	// private variables
	// rs relay state is only for the relay
	rs relayState

	// cs keeps the client state
	cs clientState

	// crocFile is the name of the file that is prepared to sent
	crocFile string
	// crocFileEncrypted is the name of the encrypted file
	crocFileEncrypted string
}

// Init will initialize the croc relay
func Init() (c *Croc) {
	c = new(Croc)
	c.TcpPorts = []string{"27030", "27031", "27032", "27033"}
	c.Timeout = 10 * time.Minute
	c.UseEncryption = true
	c.UseCompression = true
	c.AllowLocalDiscovery = true
	c.CurveType = "p521"
	c.WebsocketAddress = "wss://croc3.schollz.com"
	c.ServerPort = "8130"
	c.rs.Lock()
	c.rs.channel = make(map[string]*channelData)
	c.cs.channel = new(channelData)
	c.rs.Unlock()
	return
}

type relayState struct {
	channel map[string]*channelData
	sync.RWMutex
}

type clientState struct {
	channel *channelData
	sync.RWMutex
}

type FileMetaData struct {
	Name               string
	Size               int
	Hash               string
	IsDir              bool
	IsEncrypted        bool
	IsCompressed       bool
	DeleteAfterSending bool
}

type channelData struct {
	// Relay actions
	// Open set to true when trying to open
	Open bool `json:"open"`
	// Update set to true when updating
	Update bool `json:"update"`
	// Close set to true when closing:
	Close bool `json:"close"`

	// Public
	// Channel is the name of the channel
	Channel string `json:"channel,omitempty"`
	// Pake contains the information for
	// generating the session key over an insecure channel
	Pake *pake.Pake `json:"pake"`
	// TransferReady is set by the relaying when both parties have connected
	// with their credentials
	TransferReady bool `json:"transfer_ready"`
	// Ports returns which TCP ports to connect to
	Ports []string `json:"ports"`
	// Curve is the type of elliptic curve to use
	Curve string `json:"curve"`
	// FileMetaData is sent after confirmed
	EncryptedFileMetaData encryption `json:"encrypted_meta_data"`
	// FileReceived specifies that everything was done right
	FileReceived bool `json:"file_received"`
	// ReadyToRead means that the recipient is ready to read
	ReadyToRead bool `json:"ready_to_read"`
	// Error is sent if there is an error
	Error string `json:"error"`

	// Sent on initialization, specific to a single user
	// UUID is sent out only to one person at a time
	UUID string `json:"uuid"`
	// Role is the role the person will play
	Role int `json:"role"`

	// Private
	// client parameters
	// codePhrase uses the first 3 characters to establish a channel, and the rest
	// to form the passphrase
	codePhrase string
	// passPhrase is used to generate a session key
	passPhrase string
	// sessionKey
	sessionKey      []byte
	isReady         bool
	fileReady       bool
	fileMetaData    FileMetaData
	notSentMetaData bool
	finishedHappy   bool
	filesReady      bool
	// ws is the connection that the client has to the relay
	ws *websocket.Conn

	// relay parameters
	// isopen determine whether or not the channel has been opened
	isopen bool
	// store a UUID of the parties to prevent other parties from joining
	uuids [2]string // 0 is sender, 1 is recipient
	// connection information is stored when the clients do connect over TCP
	connection map[string][2]net.Conn
	// websocket connections
	websocketConn [2]*websocket.Conn
	// startTime is the time that the channel was opened
	startTime time.Time
}

func (cd channelData) String2() string {
	cdb, _ := json.Marshal(cd)
	return string(cdb)
}
