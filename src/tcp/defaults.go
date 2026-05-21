package tcp

import "time"

const (
	DEFAULT_LOG_LEVEL             = "debug"
	DEFAULT_ROOM_CLEANUP_INTERVAL = 10 * time.Minute
	DEFAULT_ROOM_TTL              = 3 * time.Hour
	UNAUTHORIZED_ROOM_TTL         = time.Minute
)
