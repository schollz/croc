package peerdiscovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDiscovery(t *testing.T) {
	// should not be able to "discover" itself
	discoveries, err := Discover(Settings{
		Limit:     -1,
		Payload:   []byte("payload"),
		Delay:     500 * time.Millisecond,
		TimeLimit: 5 * time.Second,
	})
	assert.Nil(t, err)
	assert.Zero(t, len(discoveries))
}
