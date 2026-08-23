package message

import (
	"encoding/json"
	"fmt"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/compress"
	"github.com/schollz/croc/v11/src/crypt"
	log "github.com/schollz/logger"
)

const maxDecompressedMessageSize = 64 * 1024 * 1024

// Type is a message type
type Type string

const (
	TypePAKE           Type = "pake"
	TypePAKEConfirm    Type = "pake-confirm"
	TypeExternalIP     Type = "externalip"
	TypeFinished       Type = "finished"
	TypeError          Type = "error"
	TypeCloseRecipient Type = "close-recipient"
	TypeCloseSender    Type = "close-sender"
	TypeRecipientReady Type = "recipientready"
	TypeFileInfo       Type = "fileinfo"
)

// Message is the possible payload for messaging
type Message struct {
	Type    Type   `json:"t,omitempty"`
	Version int    `json:"v,omitempty"`
	Message string `json:"m,omitempty"`
	Bytes   []byte `json:"b,omitempty"`
	Bytes2  []byte `json:"b2,omitempty"`
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
		log.Debugf("writing %s message (unencrypted)", m.Type)
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
	b, err = compress.Decompress(b, maxDecompressedMessageSize)
	if err != nil {
		err = fmt.Errorf("decompress message: %w", err)
		return
	}
	err = json.Unmarshal(b, &m)
	if err == nil {
		if key != nil {
			log.Debugf("read %s message (encrypted)", m.Type)
		} else {
			log.Debugf("read %s message (unencrypted)", m.Type)
		}
	}
	return
}
