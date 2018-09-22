package croc

import "time"

// Croc options
type Croc struct {
	// Options for all
	Debug bool

	// Options for relay
	ServerPort string
	CurveType  string

	// Options for connecting to server
	WebsocketAddress string
	Timeout          time.Duration
	LocalOnly        bool
	NoLocal          bool

	// Options for file transfering
	UseEncryption       bool
	UseCompression      bool
	AllowLocalDiscovery bool
	Yes                 bool
	Stdout              bool

	// private variables

	// localIP address
	localIP string
	// is using local relay
	isLocal      bool
	normalFinish bool
}
