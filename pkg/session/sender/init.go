package sender

import (
	"github.com/pion/webrtc/v2"
	log "github.com/sirupsen/logrus"
)

const (
	bufferThreshold = 512 * 1024 // 512kB
)

// Initialize creates the connection, the datachannel and creates the  offer
func (s *Session) Initialize() error {
	if s.initialized {
		return nil
	}

	if err := s.sess.CreateConnection(s.onConnectionStateChange()); err != nil {
		log.Errorln(err)
		return err
	}
	if err := s.createDataChannel(); err != nil {
		log.Errorln(err)
		return err
	}
	if err := s.sess.CreateOffer(); err != nil {
		log.Errorln(err)
		return err
	}

	s.initialized = true
	return nil
}

// Start the connection and the file transfer
func (s *Session) Start() error {
	if err := s.Initialize(); err != nil {
		return err
	}
	go s.readFile()
	if err := s.sess.ReadSDP(); err != nil {
		log.Errorln(err)
		return err
	}
	<-s.sess.Done
	s.sess.OnCompletion()
	return nil
}

func (s *Session) createDataChannel() error {
	ordered := true
	maxPacketLifeTime := uint16(10000)
	dataChannel, err := s.sess.CreateDataChannel(&webrtc.DataChannelInit{
		Ordered:           &ordered,
		MaxPacketLifeTime: &maxPacketLifeTime,
	})
	if err != nil {
		return err
	}

	s.dataChannel = dataChannel
	s.dataChannel.OnBufferedAmountLow(s.onBufferedAmountLow())
	s.dataChannel.SetBufferedAmountLowThreshold(bufferThreshold)
	s.dataChannel.OnOpen(s.onOpenHandler())
	s.dataChannel.OnClose(s.onCloseHandler())

	return nil
}
