package bench

import (
	"fmt"
	"time"

	"github.com/pion/webrtc/v2"
	log "github.com/sirupsen/logrus"
)

func (s *Session) onOpenHandlerDownload(dc *webrtc.DataChannel) func() {
	// If master, wait for the upload to complete
	// If not master, close the channel so the  upload can start
	return func() {
		if s.master {
			<-s.startPhase2
		}

		log.Debugf("Starting to download data...")
		defer log.Debugf("Stopped downloading data...")

		s.downloadNetworkStats.Start()

		// Useful for unit tests
		if dc != nil {
			dc.OnMessage(func(msg webrtc.DataChannelMessage) {
				log.Debugf("Downloading at %.2f MB/s\r", s.downloadNetworkStats.Bandwidth())
				s.downloadNetworkStats.AddBytes(uint64(len(msg.Data)))
			})
		} else {
			log.Warningln("No DataChannel provided")
		}

		timeoutErr := time.After(s.testDurationError)
		log.Debugf("Downloading random datas ... (%d s)\n", int(s.testDuration.Seconds()))

		select {
		case <-s.downloadDone:
		case <-timeoutErr:
			log.Error("Time'd out")
		}

		log.Traceln("Done downloading")

		if !s.master {
			close(s.startPhase2)
		}

		log.Debugf("\n")
		s.downloadNetworkStats.Stop()
		s.wg.Done()
	}
}

func (s *Session) onCloseHandlerDownload() func() {
	return func() {
		close(s.downloadDone)
	}
}
