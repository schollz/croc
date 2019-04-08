package session

// Session defines a common interface for sender and receiver sessions
type Session interface {
	// Start a connection and starts the file transfer
	Start() error
}
