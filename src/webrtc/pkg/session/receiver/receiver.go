package receiver

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mattn/go-colorable"
	"github.com/pion/webrtc/v2"
	"github.com/pkg/errors"
	"github.com/schollz/croc/v5/src/compress"
	"github.com/schollz/croc/v5/src/crypt"
	internalSess "github.com/schollz/croc/v5/src/webrtc/internal/session"
	"github.com/schollz/croc/v5/src/webrtc/pkg/session/common"
	"github.com/schollz/progressbar/v2"
	"github.com/schollz/spinner"
	logrus "github.com/sirupsen/logrus"
)

var log = logrus.New()

func init() {
	log.SetFormatter(&logrus.TextFormatter{ForceColors: true})
	log.SetOutput(colorable.NewColorableStdout())
	log.SetLevel(logrus.WarnLevel)
}

func Debug(debug bool) {
	if debug {
		log.SetLevel(logrus.DebugLevel)
	} else {
		log.SetLevel(logrus.WarnLevel)
	}
}

// Session is a receiver session
type Session struct {
	sess        internalSess.Session
	msgChannel  chan webrtc.DataChannelMessage
	initialized bool
	spinner     *spinner.Spinner
}

func new(s internalSess.Session) *Session {
	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = os.Stderr
	spin.Suffix = " creating channel..."
	return &Session{
		sess:        s,
		msgChannel:  make(chan webrtc.DataChannelMessage, 4096*2),
		initialized: false,
		spinner:     spin,
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
		if !s.spinner.Active() {
			s.spinner.Start()
		}
		log.Debugf("ICE Connection State has changed: %s\n", connectionState.String())
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

func (s *Session) ReceiveData(pathToFile string, fileSize int64) {
	s.receiveData(pathToFile, fileSize)
	s.sess.OnCompletion()
}

func (s *Session) receiveData(pathToFile string, fileSize int64) error {
	log.Debugln("Starting to receive data...")
	log.Debugf("receiving %s", pathToFile)

	// truncate if nessecary
	var f *os.File
	var errOpen error
	f, errOpen = os.OpenFile(pathToFile, os.O_WRONLY, 0666)
	truncate := false
	if errOpen == nil {
		stat, _ := f.Stat()
		truncate = stat.Size() != fileSize
	} else {
		f, errOpen = os.Create(pathToFile)
		if errOpen != nil {
			errOpen = errors.Wrap(errOpen, "could not create "+pathToFile)
			log.Error(errOpen)
			return errOpen
		}
		truncate = true
	}

	if truncate {
		err := f.Truncate(fileSize)
		if err != nil {
			err = errors.Wrap(err, "could not truncate "+pathToFile)
			log.Error(err)
			return err
		}
	}
	defer func() {
		log.Debugln("Stopped receiving data...")
		f.Close()
	}()

	firstByte := true
	_, fname := filepath.Split(pathToFile)
	fname = fmt.Sprintf("%10s", fname)
	var bar *progressbar.ProgressBar
	// Consume the message channel, until done
	// Does not stop on error
	for {
		select {
		case <-s.sess.Done:
			s.sess.NetworkStats.Stop()
			log.Debugf("Network: %s", s.sess.NetworkStats.String())
			log.Debug("closed gracefully")
			return nil
		case msg := <-s.msgChannel:
			buff, errDecrypt := crypt.DecryptFromBytes(msg.Data, []byte{1, 2, 3, 4})
			if errDecrypt != nil {
				log.Error(errDecrypt)
				return errDecrypt
			}
			buff = compress.Decompress(buff)
			pos := int64(binary.LittleEndian.Uint64(buff[:8]))
			n, err := f.WriteAt(buff[8:], pos)
			if err != nil {
				log.Error(err)
				return err
			} else {
				if firstByte {
					s.spinner.Stop()
					bar = progressbar.NewOptions64(
						fileSize,
						progressbar.OptionOnCompletion(func() {
							fmt.Println(" ✔️")
						}),
						progressbar.OptionSetWidth(8),
						progressbar.OptionSetDescription(fname),
						progressbar.OptionSetRenderBlankState(true),
						progressbar.OptionSetBytes64(fileSize),
						progressbar.OptionSetWriter(os.Stderr),
						progressbar.OptionThrottle(100*time.Millisecond),
					)
					firstByte = false
				}
				bar.Add(n)
				// currentSpeed := s.sess.NetworkStats.Bandwidth()
				// log.Debugf("Transferring at %.2f MB/s\r", currentSpeed)
				// s.sess.NetworkStats.AddBytes(uint64(n))
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
