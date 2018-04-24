package peerdiscovery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSettings(t *testing.T) {
	_, err := New()
	assert.Nil(t, err)

	_, err = New(Settings{
		Limit:     -1,
		Payload:   []byte("payload"),
		Delay:     500 * time.Millisecond,
		TimeLimit: 10 * time.Second,
	})
	assert.Nil(t, err)

	_, err = New(Settings{
		MulticastAddress: "assd.asdf.asdf.asfd",
	})
	assert.NotNil(t, err)
}

func TestDiscovery(t *testing.T) {
	p, _ := New(Settings{
		Limit:     -1,
		Payload:   []byte("payload"),
		Delay:     500 * time.Millisecond,
		TimeLimit: 5 * time.Second,
	})

	// should not be able to "discover" itself
	discoveries, err := p.Discover()
	assert.Nil(t, err)
	assert.Zero(t, len(discoveries))
}
