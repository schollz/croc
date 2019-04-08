package receiver

import (
	"fmt"

	"github.com/pion/webrtc/v2"
	log "github.com/sirupsen/logrus"
)

// // Initialize creates the connection, the datachannel and creates the  offer
// func (s *Session) Initialize() error {
// 	if s.initialized {
// 		return nil
// 	}
// 	if err := s.sess.CreateConnection(s.onConnectionStateChange()); err != nil {
// 		log.Errorln(err)
// 		return err
// 	}
// 	s.createDataHandler()
// 	if err := s.sess.ReadSDP(); err != nil {
// 		log.Errorln(err)
// 		return err
// 	}
// 	if err := s.sess.CreateAnswer(); err != nil {
// 		log.Errorln(err)
// 		return err
// 	}

// 	s.initialized = true
// 	return nil
// }

// // Start initializes the connection and the file transfer
// func (s *Session) Start() error {
// 	if err := s.Initialize(); err != nil {
// 		return err
// 	}

// 	// Handle data
// 	s.receiveData()
// 	s.sess.OnCompletion()
// 	return nil
// }

func (s *Session) CreateDataHandler() {
	s.sess.OnDataChannel(func(d *webrtc.DataChannel) {
		log.Debugf("New DataChannel %s %d\n", d.Label(), d.ID())
		s.sess.NetworkStats.Start()
		d.OnMessage(s.onMessage())
		d.OnClose(s.onClose())
	})
}

func (s *Session) receiveData() {
	log.Infoln("Starting to receive data...")
	defer log.Infoln("Stopped receiving data...")

	// Consume the message channel, until done
	// Does not stop on error
	for {
		select {
		case <-s.sess.Done:
			s.sess.NetworkStats.Stop()
			fmt.Printf("\nNetwork: %s\n", s.sess.NetworkStats.String())
			return
		case msg := <-s.msgChannel:
			n, err := s.stream.Write(msg.Data)

			if err != nil {
				log.Errorln(err)
			} else {
				currentSpeed := s.sess.NetworkStats.Bandwidth()
				fmt.Printf("Transferring at %.2f MB/s\r", currentSpeed)
				s.sess.NetworkStats.AddBytes(uint64(n))
			}
		}
	}
}

func (s *Session) CreateConnection() (err error) {
	return s.sess.CreateConnection(s.onConnectionStateChange())
}

func (s *Session) SetSDP(sdp string) error {
	return s.sess.SetSDP(sdp)
}

func (s *Session) CreateAnswer() (string, error) {
	return s.sess.CreateAnswer()
}
