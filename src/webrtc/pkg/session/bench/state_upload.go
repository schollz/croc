package bench

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/pion/webrtc/v2"
	log "github.com/sirupsen/logrus"
)

func (s *Session) onOpenUploadHandler(dc *webrtc.DataChannel) func() {
	return func() {
		if !s.master {
			<-s.startPhase2
		}

		log.Debugln("Starting to upload data...")
		defer log.Debugln("Stopped uploading data...")

		lenToken := uint64(4096)
		token := make([]byte, lenToken)
		if _, err := rand.Read(token); err != nil {
			log.Fatalln("Err: ", err)
		}

		s.uploadNetworkStats.Start()

		// Useful for unit tests
		if dc != nil {
			dc.SetBufferedAmountLowThreshold(s.bufferThreshold)
			dc.OnBufferedAmountLow(func() {
				if err := dc.Send(token); err == nil {
					fmt.Printf("Uploading at %.2f MB/s\r", s.uploadNetworkStats.Bandwidth())
					s.uploadNetworkStats.AddBytes(lenToken)
				}
			})
		} else {
			log.Warningln("No DataChannel provided")
		}

		fmt.Printf("Uploading random datas ... (%d s)\n", int(s.testDuration.Seconds()))
		timeout := time.After(s.testDuration)
		timeoutErr := time.After(s.testDurationError)

		if dc != nil {
			// Ignore potential error
			_ = dc.Send(token)
		}
	SENDING_LOOP:
		for {
			select {
			case <-timeoutErr:
				log.Error("Time'd out")
				break SENDING_LOOP

			case <-timeout:
				log.Traceln("Done uploading")
				break SENDING_LOOP
			}
		}
		fmt.Printf("\n")
		s.uploadNetworkStats.Stop()

		if dc != nil {
			dc.Close()
		}

		if s.master {
			close(s.startPhase2)
		}

		s.wg.Done()
	}
}
