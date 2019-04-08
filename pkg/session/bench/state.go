package bench

import (
	"github.com/pion/webrtc/v2"
	log "github.com/sirupsen/logrus"
)

func (s *Session) onConnectionStateChange() func(connectionState webrtc.ICEConnectionState) {
	return func(connectionState webrtc.ICEConnectionState) {
		log.Infof("ICE Connection State has changed: %s\n", connectionState.String())
	}
}
