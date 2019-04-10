package bench

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_TimeoutDownload(t *testing.T) {
	assert := assert.New(t)

	sess := NewWith(Config{
		Master: false,
	})

	assert.NotNil(sess)
	assert.Equal(false, sess.master)
	sess.testDurationError = 2 * time.Millisecond

	sess.wg.Add(1)
	sess.onOpenHandlerDownload(nil)()
}

func Test_TimeoutUpload(t *testing.T) {
	assert := assert.New(t)

	sess := NewWith(Config{
		Master: true,
	})

	assert.NotNil(sess)
	assert.Equal(true, sess.master)
	sess.testDurationError = 2 * time.Millisecond

	sess.wg.Add(1)
	sess.onOpenUploadHandler(nil)()
}
