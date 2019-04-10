package session

import (
	"bufio"
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_New(t *testing.T) {
	assert := assert.New(t)
	input := bufio.NewReader(&bytes.Buffer{})
	output := bufio.NewWriter(&bytes.Buffer{})

	sess := New(nil, nil)
	assert.Equal(os.Stdin, sess.sdpInput)
	assert.Equal(os.Stdout, sess.sdpOutput)

	sess = New(input, output)
	assert.Equal(input, sess.sdpInput)
	assert.Equal(output, sess.sdpOutput)
}
