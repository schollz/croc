package receiver

import (
	"github.com/pion/webrtc/v2"
	log "github.com/sirupsen/logrus"
)

func (s *Session) onConnectionStateChange() func(connectionState webrtc.ICEConnectionState) {
	return func(connectionState webrtc.ICEConnectionState) {
		log.Infof("ICE Connection State has changed: %s\n", connectionState.String())
	}
}

func (s *Session) onMessage() func(msg webrtc.DataChannelMessage) {
	return func(msg webrtc.DataChannelMessage) {
		// Store each message in the message channel
		s.msgChannel <- msg
	}
}

func (s *Session) onClose() func() {
	return func() {
		close(s.sess.Done)
	}
}
