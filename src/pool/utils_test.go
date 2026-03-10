package pool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateRelayIDFromIP(t *testing.T) {
	id1, err := GenerateRelayIDFromIP("203.0.113.10")
	assert.NoError(t, err)
	assert.Len(t, id1, 4)

	id2, err := GenerateRelayIDFromIP("203.0.113.10")
	assert.NoError(t, err)
	assert.Equal(t, id1, id2)
}

func TestParseTransferCode(t *testing.T) {
	relayID, secret := ParseTransferCode("ab12-7123-yellow-apple")
	assert.Equal(t, "ab12", relayID)
	assert.Equal(t, "7123-yellow-apple", secret)

	relayID, secret = ParseTransferCode("7123-yellow-apple")
	assert.Equal(t, "", relayID)
	assert.Equal(t, "7123-yellow-apple", secret)
}

func TestPublicIPValidation(t *testing.T) {
	assert.True(t, IsPublicIPv4("8.8.8.8"))
	assert.False(t, IsPublicIPv4("192.168.1.10"))
	assert.False(t, IsPublicIPv4("127.0.0.1"))

	assert.True(t, IsPublicIPv6("2001:4860:4860::8888"))
	assert.False(t, IsPublicIPv6("::1"))
	assert.False(t, IsPublicIPv6("fc00::1"))
}

func TestSanitizePorts(t *testing.T) {
	t.Run("trims and accepts valid", func(t *testing.T) {
		ports, err := SanitizePorts([]string{" 9009 ", "9010"})
		assert.NoError(t, err)
		assert.Equal(t, []string{"9009", "9010"}, ports)
	})

	t.Run("rejects non-numeric", func(t *testing.T) {
		_, err := SanitizePorts([]string{"9009", "foo"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be numeric")
	})

	t.Run("rejects out of range", func(t *testing.T) {
		_, err := SanitizePorts([]string{"9009", "70000"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "range 1-65535")
	})

	t.Run("rejects empty", func(t *testing.T) {
		_, err := SanitizePorts([]string{"9009", "   "})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "non-empty")
	})
}
