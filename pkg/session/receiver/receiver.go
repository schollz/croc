package receiver

import (
	"io"

	internalSess "github.com/schollz/croc/v5/internal/session"
	"github.com/schollz/croc/v5/pkg/session/common"
	"github.com/pion/webrtc/v2"
)

// Session is a receiver session
type Session struct {
	sess        internalSess.Session
	stream      io.Writer
	msgChannel  chan webrtc.DataChannelMessage
	initialized bool
}

func new(s internalSess.Session, f io.Writer) *Session {
	return &Session{
		sess:        s,
		stream:      f,
		msgChannel:  make(chan webrtc.DataChannelMessage, 4096*2),
		initialized: false,
	}
}

// New creates a new receiver session
func New(f io.Writer) *Session {
	return new(internalSess.New(nil, nil), f)
}

// Config contains custom configuration for a session
type Config struct {
	common.Configuration
	Stream io.Writer // The Stream to write to
}

// NewWith createa a new receiver Session with custom configuration
func NewWith(c Config) *Session {
	return new(internalSess.New(c.SDPProvider, c.SDPOutput), c.Stream)
}

// SetStream changes the stream, useful for WASM integration
func (s *Session) SetStream(stream io.Writer) {
	s.stream = stream
}
