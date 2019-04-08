package session

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/schollz/croc/internal/buffer"
	"github.com/schollz/croc/pkg/session/common"
	"github.com/schollz/croc/pkg/session/receiver"
	"github.com/schollz/croc/pkg/session/sender"
	"github.com/schollz/croc/pkg/utils"
	"github.com/stretchr/testify/assert"
)

// Tests

func Test_CreateReceiverSession(t *testing.T) {
	assert := assert.New(t)
	stream := &bytes.Buffer{}

	sess := receiver.NewWith(receiver.Config{
		Stream: stream,
	})
	assert.NotNil(sess)
}

func Test_TransferSmallMessage(t *testing.T) {
	assert := assert.New(t)

	// Create client receiver
	clientStream := &buffer.Buffer{}
	clientSDPProvider := &buffer.Buffer{}
	clientSDPOutput := &buffer.Buffer{}
	clientConfig := receiver.Config{
		Stream: clientStream,
		Configuration: common.Configuration{
			SDPProvider: clientSDPProvider,
			SDPOutput:   clientSDPOutput,
		},
	}
	clientSession := receiver.NewWith(clientConfig)
	assert.NotNil(clientSession)

	// Create sender
	senderStream := &buffer.Buffer{}
	senderSDPProvider := &buffer.Buffer{}
	senderSDPOutput := &buffer.Buffer{}
	n, err := senderStream.WriteString("Hello World!\n")
	assert.Nil(err)
	assert.Equal(13, n) // Len "Hello World\n"
	senderConfig := sender.Config{
		Stream: senderStream,
		Configuration: common.Configuration{
			SDPProvider: senderSDPProvider,
			SDPOutput:   senderSDPOutput,
		},
	}
	senderSession := sender.NewWith(senderConfig)
	assert.NotNil(senderSession)

	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		err := senderSession.Start()
		assert.Nil(err)
	}()

	// Get SDP from sender and send it to the client
	sdp, err := utils.MustReadStream(senderSDPOutput)
	assert.Nil(err)
	fmt.Printf("READ SDP -> %s\n", sdp)
	sdp += "\n"
	n, err = clientSDPProvider.WriteString(sdp)
	assert.Nil(err)
	assert.Equal(len(sdp), n)

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		err := clientSession.Start()
		assert.Nil(err)
	}()

	// Get SDP from client and send it to the sender
	sdp, err = utils.MustReadStream(clientSDPOutput)
	assert.Nil(err)
	n, err = senderSDPProvider.WriteString(sdp)
	assert.Nil(err)
	assert.Equal(len(sdp), n)

	fmt.Println("Waiting for everyone to be done...")
	<-senderDone
	<-clientDone

	msg, err := clientStream.ReadString('\n')
	assert.Nil(err)
	assert.Equal("Hello World!\n", msg)
}
