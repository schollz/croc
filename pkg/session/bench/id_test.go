package bench

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_IDs(t *testing.T) {
	assert := assert.New(t)

	sess := NewWith(Config{
		Master: false,
	})
	assert.NotNil(sess)
	assert.Equal(false, sess.master)

	sessMaster := NewWith(Config{
		Master: true,
	})
	assert.NotNil(sessMaster)
	assert.Equal(true, sessMaster.master)

	assert.Equal(sessMaster.downloadChannelID(), sess.uploadChannelID())
	assert.Equal(sessMaster.uploadChannelID(), sess.downloadChannelID())
	assert.NotEqual(sessMaster.downloadChannelID(), sess.downloadChannelID())

}
