package receiver

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_New(t *testing.T) {
	assert := assert.New(t)
	output := bufio.NewWriter(&bytes.Buffer{})

	sess := New(output)

	assert.NotNil(sess)
	assert.Equal(output, sess.stream)
}
