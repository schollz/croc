package bench

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_OnNewDataChannel(t *testing.T) {
	assert := assert.New(t)
	testDuration := 2 * time.Second

	sess := NewWith(Config{
		Master: false,
	})
	assert.NotNil(sess)
	sess.testDuration = testDuration
	sess.testDurationError = (testDuration * 10) / 8

	sess.onNewDataChannel()(nil)

	testID := sess.uploadChannelID()
	sess.onNewDataChannelHelper("", testID, nil)

	testID = sess.uploadChannelID() | sess.downloadChannelID()
	sess.onNewDataChannelHelper("", testID, nil)
}
