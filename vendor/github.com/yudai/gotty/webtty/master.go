package webtty

import (
	"io"
)

// Master represents a PTY master, usually it's a websocket connection.
type Master io.ReadWriter
