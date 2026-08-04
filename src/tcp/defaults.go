package tcp

import "time"

const (
	DEFAULT_LOG_LEVEL              = "debug"
	DEFAULT_MAX_PENDING_HANDSHAKES = 64
	DEFAULT_HANDSHAKE_TIMEOUT      = 5 * time.Minute
	DEFAULT_MAX_ROOMS_OPEN         = 128
	DEFAULT_ROOM_CLEANUP_INTERVAL  = 10 * time.Minute
	DEFAULT_ROOM_TTL               = 3 * time.Hour
)
