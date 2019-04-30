package message

import (
	"fmt"
	"testing"

	"github.com/schollz/croc/v6/src/crypt"
	"github.com/stretchr/testify/assert"
)

func TestMessage(t *testing.T) {
	m := Message{Type: "message", Message: "hello, world"}
	e, err := crypt.New([]byte("passphrase"), nil)
	assert.Nil(t, err)
	fmt.Println(e.Salt())
	b, err := Encode(m, e)
	assert.Nil(t, err)
	fmt.Printf("%x\n", b)

	m2, err := Decode(b, e)
	assert.Nil(t, err)
	assert.Equal(t, m, m2)
}
