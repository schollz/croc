package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExpiration(t *testing.T) {
	tests := map[string]time.Duration{
		"1m":  time.Minute,
		"90m": 90 * time.Minute,
		"12h": 12 * time.Hour,
		"3d":  72 * time.Hour,
		"2w":  14 * 24 * time.Hour,
	}
	for input, want := range tests {
		got, err := ParseExpiration(input, false)
		require.NoError(t, err, input)
		assert.Equal(t, want, got, input)
	}
}

func TestParseExpirationRejectsInvalidAndNonPositiveValues(t *testing.T) {
	for _, input := range []string{"", "0", "0m", "-1h", "30s", "1.5h", "1h30m", "1D", "9223372036854775807w"} {
		_, err := ParseExpiration(input, false)
		assert.Error(t, err, input)
	}
}

func TestParseExpirationUnlimitedServerValue(t *testing.T) {
	for _, input := range []string{"", "0", " 0 "} {
		got, err := ParseExpiration(input, true)
		require.NoError(t, err, input)
		assert.Zero(t, got, input)
	}
	_, err := ParseExpiration("0m", true)
	assert.Error(t, err)
}
