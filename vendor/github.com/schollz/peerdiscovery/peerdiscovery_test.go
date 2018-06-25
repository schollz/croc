package peerdiscovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDiscovery(t *testing.T) {
	// should not be able to "discover" itself
	discoveries, err := Discover()
	assert.Nil(t, err)
	assert.Zero(t, len(discoveries))

	// should be able to "discover" itself
	discoveries, err = Discover(Settings{
		Limit:     -1,
		AllowSelf: true,
		Payload:   []byte("payload"),
		Delay:     500 * time.Millisecond,
		TimeLimit: 1 * time.Second,
	})
	assert.Nil(t, err)
	assert.NotZero(t, len(discoveries))
}
