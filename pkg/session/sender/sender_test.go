package sender

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_New(t *testing.T) {
	assert := assert.New(t)
	input := bufio.NewReader(&bytes.Buffer{})

	sess := New(input)

	assert.NotNil(sess)
	assert.Equal(input, sess.stream)
}
