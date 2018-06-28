package croc

import (
	"crypto/elliptic"
	"net"
	"sync"
	"time"
)

const (
	// maximum buffer size for initial TCP communication
	bufferSize = 1024
)

var (
	// availableStates are the states available to the parties involved
	availableStates = []string{"curve", "h_k", "hh_k", "x", "y"}
)

type relayState struct {
	channel map[string]*channelData
	sync.RWMutex
}

type channelData struct {
	// Public
	// Name is the name of the channel
	Name string `json:"name,omitempty"`
	// State contains state variables that are public to both parties
	State map[string][]byte `json:"state"`
	// TransferReady is set by the relaying when both parties have connected
	// with their credentials
	TransferReady bool `json:"transfer_ready"`
	// Ports returns which TCP ports to connect to
	Ports []string `json:"ports"`

	// Private
	// isopen determine whether or not the channel has been opened
	isopen bool
	// store a UUID of the parties to prevent other parties from joining
	uuids [2]string // 0 is sender, 1 is recipient
	// curve is the type of elliptic curve used for PAKE
	curve elliptic.Curve
	// connection information is stored when the clients do connect over TCP
	connection [2]net.Conn
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

type payloadOpen struct {
	// Channel is used to designate the channel of interest
	Channel string `json:"channel"`
	// Role designates which role the person will take;
	// 0 for sender and 1 for recipient.
	Role int `json:"role"`
	// Curve is the curve to be used.
	Curve string `json:"curve"`
}

type payloadChannel struct {
	Channel string            `json:"channel" binding:"required"`
	UUID    string            `json:"uuid" binding:"required"`
	State   map[string][]byte `json:"state"`
	Close   bool              `json:"close"`
}

func newChannelData(name string) (cd *channelData) {
	cd = new(channelData)
	cd.Name = name
	cd.State = make(map[string][]byte)
	for _, state := range availableStates {
		cd.State[state] = []byte{}
	}
	return
}
