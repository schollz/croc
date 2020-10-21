package message

import (
	"encoding/json"

	"github.com/schollz/croc/v8/src/comm"
	"github.com/schollz/croc/v8/src/compress"
	"github.com/schollz/croc/v8/src/crypt"
	log "github.com/schollz/logger"
)

// Message is the possible payload for messaging
type Message struct {
	Type    string `json:"t,omitempty"`
	Message string `json:"m,omitempty"`
	Bytes   []byte `json:"b,omitempty"`
	Num     int    `json:"n,omitempty"`
}

func (m Message) String() string {
	b, _ := json.Marshal(m)
	return string(b)
}

// Send will send out
func Send(c *comm.Comm, key []byte, m Message) (err error) {
	mSend, err := Encode(key, m)
	if err != nil {
		return
	}
	err = c.Send(mSend)
	return
}

// Encode will convert to bytes
func Encode(key []byte, m Message) (b []byte, err error) {
	b, err = json.Marshal(m)
	if err != nil {
		return
	}
	b = compress.Compress(b)
	if key != nil {
		log.Debugf("writing %s message (encrypted)", m.Type)
		b, err = crypt.Encrypt(b, key)
	} else {
		log.Debugf("writing %s message", m.Type)
	}
	return
}

// Decode will convert from bytes
func Decode(key []byte, b []byte) (m Message, err error) {
	if key != nil {
		b, err = crypt.Decrypt(b, key)
		if err != nil {
			return
		}
	}
	b = compress.Decompress(b)
	err = json.Unmarshal(b, &m)
	return
}
