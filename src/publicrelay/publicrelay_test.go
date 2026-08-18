package publicrelay

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelaysReturnsProtocolOrderAndCopy(t *testing.T) {
	got := Relays()
	assert.Equal(t, []string{
		"1.getcroc.com:9009",
		"2.getcroc.com:9009",
		"3.getcroc.com:9009",
	}, got)
	got[0] = "changed"
	assert.Equal(t, "1.getcroc.com:9009", Relays()[0])
}

func TestSelectFirstReturnsFirstSuccessfulProbeAndCancelsOthers(t *testing.T) {
	canceled := make(chan string, 2)
	probe := func(ctx context.Context, address string, _ time.Duration) (time.Duration, error) {
		if address == "fast" {
			time.Sleep(10 * time.Millisecond)
			return 10 * time.Millisecond, nil
		}
		select {
		case <-ctx.Done():
			canceled <- address
			return 0, ctx.Err()
		case <-time.After(time.Second):
			return 0, errors.New("probe was not canceled")
		}
	}

	started := time.Now()
	best, duration, err := SelectFirst(context.Background(), []string{"slow", "fast", "slower"}, time.Second, probe)
	require.NoError(t, err)
	assert.Equal(t, 1, best)
	assert.Equal(t, 10*time.Millisecond, duration)
	assert.Less(t, time.Since(started), 250*time.Millisecond)
	require.Eventually(t, func() bool { return len(canceled) == 2 }, time.Second, time.Millisecond)
}

func TestSelectFirstIgnoresFailuresBeforeSuccess(t *testing.T) {
	probe := func(_ context.Context, address string, _ time.Duration) (time.Duration, error) {
		if address == "working" {
			time.Sleep(10 * time.Millisecond)
			return 10 * time.Millisecond, nil
		}
		return 0, errors.New("unavailable")
	}
	best, _, err := SelectFirst(context.Background(), []string{"failed", "working"}, time.Second, probe)
	require.NoError(t, err)
	assert.Equal(t, 1, best)
}

func TestSelectFirstFailsWithoutHealthyRelay(t *testing.T) {
	probe := func(context.Context, string, time.Duration) (time.Duration, error) {
		return 0, errors.New("unavailable")
	}
	_, _, err := SelectFirst(context.Background(), []string{"one", "two"}, time.Second, probe)
	assert.EqualError(t, err, "no public relay available")
}

func TestSelectFirstValidatesArguments(t *testing.T) {
	probe := func(context.Context, string, time.Duration) (time.Duration, error) { return 0, nil }
	_, _, err := SelectFirst(context.Background(), nil, time.Second, probe)
	assert.EqualError(t, err, "public relay pool is empty")
	_, _, err = SelectFirst(context.Background(), []string{"one"}, 0, probe)
	assert.EqualError(t, err, "probe timeout must be positive")
	_, _, err = SelectFirst(context.Background(), []string{"one"}, time.Second, nil)
	assert.EqualError(t, err, "relay probe is required")
	_, _, err = SelectFirst(nil, []string{"one"}, time.Second, probe)
	assert.EqualError(t, err, "probe context is required")
}
