package sender

import (
	"sync"

	"github.com/pion/webrtc/v2"
	internalSess "github.com/schollz/croc/v5/internal/session"
	"github.com/schollz/croc/v5/pkg/session/common"
	"github.com/schollz/croc/v5/pkg/stats"
)

const (
	// Must be <= 16384
	senderBuffSize = 16384
)

type outputMsg struct {
	n    int
	buff []byte
}

// Session is a sender session
type Session struct {
	sess        internalSess.Session
	initialized bool

	dataChannel *webrtc.DataChannel
	dataBuff    []byte
	msgToBeSent []outputMsg
	stopSending chan struct{}
	output      chan outputMsg

	doneCheckLock sync.Mutex
	doneCheck     bool

	// Stats/infos
	readingStats *stats.Stats
}

// New creates a new sender session
func new(s internalSess.Session) *Session {
	return &Session{
		sess:         s,
		initialized:  false,
		dataBuff:     make([]byte, senderBuffSize),
		stopSending:  make(chan struct{}, 1),
		output:       make(chan outputMsg, senderBuffSize*10),
		doneCheck:    false,
		readingStats: stats.New(),
	}
}

// New creates a new receiver session
func New() *Session {
	return new(internalSess.New(nil, nil))
}

// Config contains custom configuration for a session
type Config struct {
	common.Configuration
}

// NewWith createa a new sender Session with custom configuration
func NewWith(c Config) *Session {
	return new(internalSess.New(c.SDPProvider, c.SDPOutput))
}

func (s *Session) CreateConnection() (err error) {
	return s.sess.CreateConnection(s.onConnectionStateChange())
}

func (s *Session) CreateOffer() (string, error) {
	return s.sess.CreateOffer()
}

func (s *Session) SetSDP(sdp string) error {
	return s.sess.SetSDP(sdp)
}

func (s *Session) TransferFile(pathToFile string) {
	s.readFile(pathToFile)
	s.sess.OnCompletion()
}
