package receiver

import (
	"io"

	"github.com/pion/webrtc/v2"
	internalSess "github.com/schollz/croc/v5/internal/session"
	"github.com/schollz/croc/v5/pkg/session/common"
)

// Session is a receiver session
type Session struct {
	sess        internalSess.Session
	msgChannel  chan webrtc.DataChannelMessage
	initialized bool
}

func new(s internalSess.Session) *Session {
	return &Session{
		sess:        s,
		msgChannel:  make(chan webrtc.DataChannelMessage, 4096*2),
		initialized: false,
	}
}

// New creates a new receiver session
func New() *Session {
	return new(internalSess.New(nil, nil))
}

// Config contains custom configuration for a session
type Config struct {
	common.Configuration
	Stream io.Writer // The Stream to write to
}

// NewWith createa a new receiver Session with custom configuration
func NewWith(c Config) *Session {
	return new(internalSess.New(c.SDPProvider, c.SDPOutput))
}
