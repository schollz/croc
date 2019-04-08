package bench

import (
	"fmt"

	"github.com/pion/webrtc/v2"
	log "github.com/sirupsen/logrus"
)

// Start initializes the connection and the benchmark
func (s *Session) Start() error {
	if err := s.sess.CreateConnection(s.onConnectionStateChange()); err != nil {
		log.Errorln(err)
		return err
	}

	s.sess.OnDataChannel(s.onNewDataChannel())
	if err := s.createUploadDataChannel(); err != nil {
		log.Errorln(err)
		return err
	}

	s.wg.Add(2) // Download + Upload
	if s.master {
		if err := s.createMasterSession(); err != nil {
			return err
		}
	} else {
		if err := s.createSlaveSession(); err != nil {
			return err
		}
	}
	// Wait for benchmarks to be done
	s.wg.Wait()

	fmt.Printf("Upload:   %s\n", s.uploadNetworkStats.String())
	fmt.Printf("Download: %s\n", s.downloadNetworkStats.String())
	s.sess.OnCompletion()
	return nil
}

func (s *Session) initDataChannel(channelID *uint16) (*webrtc.DataChannel, error) {
	ordered := true
	maxPacketLifeTime := uint16(10000)
	return s.sess.CreateDataChannel(&webrtc.DataChannelInit{
		Ordered:           &ordered,
		MaxPacketLifeTime: &maxPacketLifeTime,
		ID:                channelID,
	})
}

func (s *Session) createUploadDataChannel() error {
	channelID := s.uploadChannelID()
	dataChannel, err := s.initDataChannel(&channelID)
	if err != nil {
		return err
	}

	dataChannel.OnOpen(s.onOpenUploadHandler(dataChannel))

	return nil
}
