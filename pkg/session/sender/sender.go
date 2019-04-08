package sender

import (
	"io"
	"sync"

	"github.com/pion/webrtc/v2"
	internalSess "github.com/schollz/croc/internal/session"
	"github.com/schollz/croc/pkg/session/common"
	"github.com/schollz/croc/pkg/stats"
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
	stream      io.Reader
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
func new(s internalSess.Session, f io.Reader) *Session {
	return &Session{
		sess:         s,
		stream:       f,
		initialized:  false,
		dataBuff:     make([]byte, senderBuffSize),
		stopSending:  make(chan struct{}, 1),
		output:       make(chan outputMsg, senderBuffSize*10),
		doneCheck:    false,
		readingStats: stats.New(),
	}
}

// New creates a new receiver session
func New(f io.Reader) *Session {
	return new(internalSess.New(nil, nil), f)
}

// Config contains custom configuration for a session
type Config struct {
	common.Configuration
	Stream io.Reader // The Stream to read from
}

// NewWith createa a new sender Session with custom configuration
func NewWith(c Config) *Session {
	return new(internalSess.New(c.SDPProvider, c.SDPOutput), c.Stream)
}

// SetStream changes the stream, useful for WASM integration
func (s *Session) SetStream(stream io.Reader) {
	s.stream = stream
}
