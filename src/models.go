package croc

import (
	"crypto/elliptic"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// maximum buffer size for initial TCP communication
	bufferSize = 1024
)

var (
	// see PAKE setup for more info: https://play.golang.org/p/QLHvINK4qFG
	// availableStates are the varaibles available to the parties involved
	availableStates = []string{"curve", "Xᵤ", "Xᵥ", "Yᵤ", "Yᵥ", "Uᵤ", "Uᵥ", "Vᵤ", "Vᵥ", "HHBk", "HAk"}
	// availableSecrets are the variables available only to a specific client, and not shared
	availableSecrets = []string{"pw", "Upwᵤ", "Upwᵥ", "α", "αᵤ", "αᵥ", "Vpwᵤ", "Vpwᵥ", "β", "gβᵤ", "gβᵥ", "BZᵤ", "BZᵥ", "BZᵤ", "BZᵥ", "AZᵤ", "AZᵥ", "AZᵤ", "AZᵥ", "Bk", "Ak"}
)

type Croc struct {
	TcpPorts            []string
	ServerPort          string
	Timeout             time.Duration
	UseEncryption       bool
	UseCompression      bool
	CurveType           string
	AllowLocalDiscovery bool

	// private variables
	// rs relay state is only for the relay
	rs relayState

	// cs keeps the client state
	cs clientState
}

// Init will initialize the croc relay
func Init() (c *Croc) {
	c = new(Croc)
	c.TcpPorts = []string{"27001", "27002", "27003", "27004"}
	c.ServerPort = "8003"
	c.Timeout = 10 * time.Minute
	c.UseEncryption = true
	c.UseCompression = true
	c.AllowLocalDiscovery = true
	c.CurveType = "p521"
	c.rs.Lock()
	c.rs.channel = make(map[string]*channelData)
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

type channelData struct {
	// Public
	// Channel is the name of the channel
	Channel string `json:"channel,omitempty"`
	// State contains state variables that are public to both parties
	// contains "curve", "h_k", "hh_k", "x", "y"
	State map[string][]byte `json:"state"`
	// TransferReady is set by the relaying when both parties have connected
	// with their credentials
	TransferReady bool `json:"transfer_ready"`
	// Ports returns which TCP ports to connect to
	Ports []string `json:"ports"`

	// Error is sent if there is an error
	Error string `json:"error"`

	// Sent on initialization, specific to a single user
	// UUID is sent out only to one person at a time
	UUID string `json:"uuid"`
	// Role is the role the person will play
	Role int `json:"role"`

	// Private
	// client parameters

	// secret are the computed secretes
	// contains "curve", "h_k", "hh_k", "x", "y"
	secret map[string][]byte `json:"secret"`

	// relay + client parameters
	// curve is the type of elliptic curve used for PAKE
	curve elliptic.Curve

	// relay parameters
	// isopen determine whether or not the channel has been opened
	isopen bool
	// store a UUID of the parties to prevent other parties from joining
	uuids [2]string // 0 is sender, 1 is recipient
	// connection information is stored when the clients do connect over TCP
	connection [2]net.Conn
	// websocket connections
	websocketConn [2]*websocket.Conn
	// startTime is the time that the channel was opened
	startTime time.Time
}

type response struct {
	// various responses
	Channel string       `json:"channel,omitempty"`
	UUID    string       `json:"uuid,omitempty"`
	Data    *channelData `json:"data,omitempty"`

	// constant responses
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type payload struct {
	// Open set to true when trying to open
	Open bool `json:"open"`
	// Channel is used to designate the channel of interest
	Channel string `json:"channel"`
	// Role designates which role the person will take;
	// 0 for sender and 1 for recipient.
	Role int `json:"role"`
	// Curve is the curve to be used.
	Curve string `json:"curve"`

	// Update set to true when updating
	Update bool   `json:"update"`
	UUID   string `json:"uuid"`
	// State is the state information to be updated
	State map[string][]byte `json:"state"`

	// Close set to true when closing:
	Close bool `json:"close"`
}

func newChannelData(name string) (cd *channelData) {
	cd = new(channelData)
	cd.Channel = name
	cd.State = make(map[string][]byte)
	for _, s := range availableStates {
		cd.State[s] = []byte{}
	}
	cd.secret = make(map[string][]byte)
	for _, s := range availableSecrets {
		cd.secret[s] = []byte{}
	}
	return
}
