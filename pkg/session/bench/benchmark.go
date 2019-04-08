package bench

import (
	"sync"
	"time"

	internalSess "github.com/schollz/croc/internal/session"
	"github.com/schollz/croc/pkg/session/common"
	"github.com/schollz/croc/pkg/stats"
)

const (
	bufferThresholdDefault   = 64 * 1024 // 64kB
	testDurationDefault      = 20 * time.Second
	testDurationErrorDefault = (testDurationDefault * 10) / 7
)

// Session is a benchmark session
type Session struct {
	sess   internalSess.Session
	master bool
	wg     sync.WaitGroup

	// Settings
	bufferThreshold   uint64
	testDuration      time.Duration
	testDurationError time.Duration

	startPhase2          chan struct{}
	uploadNetworkStats   *stats.Stats
	downloadDone         chan bool
	downloadNetworkStats *stats.Stats
}

// New creates a new sender session
func new(s internalSess.Session, isMaster bool) *Session {
	return &Session{
		sess:   s,
		master: isMaster,

		bufferThreshold:   bufferThresholdDefault,
		testDuration:      testDurationDefault,
		testDurationError: testDurationErrorDefault,

		startPhase2:          make(chan struct{}),
		downloadDone:         make(chan bool),
		uploadNetworkStats:   stats.New(),
		downloadNetworkStats: stats.New(),
	}
}

// Config contains custom configuration for a session
type Config struct {
	common.Configuration
	Master bool // Will create the SDP offer ?
}

// NewWith createa a new benchmark Session with custom configuration
func NewWith(c Config) *Session {
	return new(internalSess.New(c.SDPProvider, c.SDPOutput), c.Master)
}
