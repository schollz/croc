package sender

import (
	"fmt"
	"io"

	log "github.com/sirupsen/logrus"
)

func (s *Session) readFile() {
	log.Infof("Starting to read data...")
	s.readingStats.Start()
	defer func() {
		s.readingStats.Pause()
		log.Infof("Stopped reading data...")
		close(s.output)
	}()

	for {
		// Read file
		s.dataBuff = s.dataBuff[:cap(s.dataBuff)]
		n, err := s.stream.Read(s.dataBuff)
		if err != nil {
			if err == io.EOF {
				s.readingStats.Stop()
				log.Debugf("Got EOF after %v bytes!\n", s.readingStats.Bytes())
				return
			}
			log.Errorf("Read Error: %v\n", err)
			return
		}
		s.dataBuff = s.dataBuff[:n]
		s.readingStats.AddBytes(uint64(n))

		s.output <- outputMsg{
			n: n,
			// Make a copy of the buffer
			buff: append([]byte(nil), s.dataBuff...),
		}
	}
}

func (s *Session) onBufferedAmountLow() func() {
	return func() {
		data := <-s.output
		if data.n != 0 {
			s.msgToBeSent = append(s.msgToBeSent, data)
		} else if len(s.msgToBeSent) == 0 && s.dataChannel.BufferedAmount() == 0 {
			s.sess.NetworkStats.Stop()
			s.close(false)
			return
		}

		currentSpeed := s.sess.NetworkStats.Bandwidth()
		fmt.Printf("Transferring at %.2f MB/s\r", currentSpeed)

		for len(s.msgToBeSent) != 0 {
			cur := s.msgToBeSent[0]

			if err := s.dataChannel.Send(cur.buff); err != nil {
				log.Errorf("Error, cannot send to client: %v\n", err)
				return
			}
			s.sess.NetworkStats.AddBytes(uint64(cur.n))
			s.msgToBeSent = s.msgToBeSent[1:]
		}
	}
}

func (s *Session) writeToNetwork() {
	// Set callback, as transfer may be paused
	s.dataChannel.OnBufferedAmountLow(s.onBufferedAmountLow())

	<-s.stopSending
	s.dataChannel.OnBufferedAmountLow(nil)
	s.sess.NetworkStats.Pause()
	log.Infof("Pausing network I/O... (remaining at least %v packets)\n", len(s.output))
}
