package bench

import (
	"testing"
	"time"

	"github.com/schollz/croc/v5/src/webrtc/internal/buffer"
	"github.com/schollz/croc/v5/src/webrtc/pkg/session/common"
	"github.com/schollz/croc/v5/src/webrtc/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func Test_New(t *testing.T) {
	assert := assert.New(t)

	sess := NewWith(Config{
		Master: false,
	})

	assert.NotNil(sess)
	assert.Equal(false, sess.master)
}

func Test_Bench(t *testing.T) {
	assert := assert.New(t)

	sessionSDPProvider := &buffer.Buffer{}
	sessionSDPOutput := &buffer.Buffer{}
	sessionMasterSDPProvider := &buffer.Buffer{}
	sessionMasterSDPOutput := &buffer.Buffer{}

	testDuration := 2 * time.Second

	sess := NewWith(Config{
		Configuration: common.Configuration{
			SDPProvider: sessionSDPProvider,
			SDPOutput:   sessionSDPOutput,
		},
		Master: false,
	})
	assert.NotNil(sess)
	sess.testDuration = testDuration
	sess.testDurationError = (testDuration * 10) / 8

	sessMaster := NewWith(Config{
		Configuration: common.Configuration{
			SDPProvider: sessionMasterSDPProvider,
			SDPOutput:   sessionMasterSDPOutput,
		},
		Master: true,
	})
	assert.NotNil(sessMaster)
	sessMaster.testDuration = testDuration
	sessMaster.testDurationError = (testDuration * 10) / 8

	masterDone := make(chan struct{})
	go func() {
		defer close(masterDone)
		err := sessMaster.Start()
		assert.Nil(err)
	}()

	sdp, err := utils.MustReadStream(sessionMasterSDPOutput)
	assert.Nil(err)
	sdp += "\n"
	n, err := sessionSDPProvider.WriteString(sdp)
	assert.Nil(err)
	assert.Equal(len(sdp), n)

	slaveDone := make(chan struct{})
	go func() {
		defer close(slaveDone)
		err := sess.Start()
		assert.Nil(err)
	}()

	// Get SDP from slave and send it to the master
	sdp, err = utils.MustReadStream(sessionSDPOutput)
	assert.Nil(err)
	n, err = sessionMasterSDPProvider.WriteString(sdp)
	assert.Nil(err)
	assert.Equal(len(sdp), n)

	<-masterDone
	<-slaveDone
}
