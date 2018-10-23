package croc

type WebSocketMessage struct {
	messageType int
	message     []byte
	err         error
}
