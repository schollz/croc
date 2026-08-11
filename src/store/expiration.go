package store

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultExpiration preserves the historical stored-transfer lifetime.
	DefaultExpiration = 24 * time.Hour
	// MinExpiration is the shortest lifetime accepted by clients and servers.
	MinExpiration = time.Minute
	// MaxExpirationSeconds is the longest whole-second lifetime representable
	// by time.Duration.
	MaxExpirationSeconds = int64(math.MaxInt64 / int64(time.Second))
)

// ParseExpiration parses the stored-transfer duration syntax used by the CLI.
// Values are whole minutes, hours, days, or weeks. When allowUnlimited is true,
// an empty value or the literal "0" represents no server policy ceiling.
func ParseExpiration(value string, allowUnlimited bool) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if allowUnlimited && (value == "" || value == "0") {
		return 0, nil
	}
	if len(value) < 2 {
		return 0, errors.New("duration must be a positive whole number followed by m, h, d, or w")
	}

	unit := value[len(value)-1]
	var multiplier time.Duration
	switch unit {
	case 'm':
		multiplier = time.Minute
	case 'h':
		multiplier = time.Hour
	case 'd':
		multiplier = 24 * time.Hour
	case 'w':
		multiplier = 7 * 24 * time.Hour
	default:
		return 0, errors.New("duration unit must be m, h, d, or w")
	}

	amount, err := strconv.ParseUint(value[:len(value)-1], 10, 64)
	if err != nil || amount == 0 {
		return 0, errors.New("duration must be a positive whole number")
	}
	if amount > uint64(math.MaxInt64/int64(multiplier)) {
		return 0, fmt.Errorf("duration %q is too large", value)
	}
	duration := time.Duration(amount) * multiplier
	if duration < MinExpiration {
		return 0, fmt.Errorf("duration must be at least %s", MinExpiration)
	}
	return duration, nil
}
