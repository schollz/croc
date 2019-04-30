package message

import (
	"encoding/json"

	"github.com/schollz/croc/v6/src/compress"
	"github.com/schollz/croc/v6/src/crypt"
	"github.com/schollz/croc/v6/src/comm"
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
func Send(c *comm.Comm, m Message) (err error) {
	mSend, err := Encode(m)
	if err != nil {
		return
	}
	_, err = c.Write(mSend)
return
}

// Encode will convert to bytes
func Encode(m Message, e ...crypt.Encryption) (b []byte, err error) {
	b, err = json.Marshal(m)
	if err != nil {
		return
	}

	b = compress.Compress(b)
	if len(e) > 0 {
		b, err = e[0].Encrypt(b)
	}
	return
}

// Decode will convert from bytes
func Decode(b []byte, e ...crypt.Encryption) (m Message, err error) {
	if len(e) > 0 {
		b, err = e[0].Decrypt(b)
		if err != nil {
			return
		}
	}
	b = compress.Decompress(b)
	err = json.Unmarshal(b, &m)
	return
}
