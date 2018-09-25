package models

const WEBSOCKET_BUFFER_SIZE = 1024 * 1024 * 32
const TCP_BUFFER_SIZE = 1024 * 64

type BytesAndLocation struct {
	Bytes    []byte `json:"b"`
	Location int64  `json:"l"`
}
