package receiver

import (
	"fmt"
	"io"
	"os"

	"github.com/pion/webrtc/v2"
	internalSess "github.com/schollz/croc/v5/internal/session"
	"github.com/schollz/croc/v5/pkg/session/common"
	log "github.com/sirupsen/logrus"
)

// Session is a receiver session
type Session struct {
	sess        internalSess.Session
	msgChannel  chan webrtc.DataChannelMessage
	initialized bool
}

func new(s internalSess.Session) *Session {
	return &Session{
		sess:        s,
		msgChannel:  make(chan webrtc.DataChannelMessage, 4096*2),
		initialized: false,
	}
}

// New creates a new receiver session
func New() *Session {
	return new(internalSess.New(nil, nil))
}

// Config contains custom configuration for a session
type Config struct {
	common.Configuration
	Stream io.Writer // The Stream to write to
}

// NewWith createa a new receiver Session with custom configuration
func NewWith(c Config) *Session {
	return new(internalSess.New(c.SDPProvider, c.SDPOutput))
}

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

func (s *Session) ReceiveData(pathToFile string) {
	s.receiveData(pathToFile)
	s.sess.OnCompletion()
}

func (s *Session) receiveData(pathToFile string) error {
	log.Infoln("Starting to receive data...")
	f, err := os.OpenFile(pathToFile, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return err
	}
	defer func() {
		log.Infoln("Stopped receiving data...")
		f.Close()
	}()
	// Consume the message channel, until done
	// Does not stop on error
	for {
		select {
		case <-s.sess.Done:
			s.sess.NetworkStats.Stop()
			fmt.Printf("\nNetwork: %s\n", s.sess.NetworkStats.String())
			return nil
		case msg := <-s.msgChannel:
			n, err := f.Write(msg.Data)

			if err != nil {
				return err
			} else {
				currentSpeed := s.sess.NetworkStats.Bandwidth()
				fmt.Printf("Transferring at %.2f MB/s\r", currentSpeed)
				s.sess.NetworkStats.AddBytes(uint64(n))
			}
		}
	}
	return nil
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
