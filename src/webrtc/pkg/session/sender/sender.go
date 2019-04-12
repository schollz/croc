package sender

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	colorable "github.com/mattn/go-colorable"
	"github.com/pion/webrtc/v2"
	"github.com/schollz/croc/v5/src/compress"
	"github.com/schollz/croc/v5/src/crypt"
	internalSess "github.com/schollz/croc/v5/src/webrtc/internal/session"
	"github.com/schollz/croc/v5/src/webrtc/pkg/session/common"
	"github.com/schollz/croc/v5/src/webrtc/pkg/stats"
	"github.com/schollz/progressbar/v2"
	"github.com/schollz/spinner"
	"github.com/sirupsen/logrus"
)

const (
	// Must be <= 16384
	// 8 bytes for position
	// 3000 bytes for encryption / compression overhead
	senderBuffSize  = 8192
	bufferThreshold = 512 * 1024 // 512kB
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

type outputMsg struct {
	n    int
	buff []byte
}

// Session is a sender session
type Session struct {
	sess        internalSess.Session
	initialized bool

	dataChannel *webrtc.DataChannel
	dataBuff    []byte
	msgToBeSent []outputMsg
	stopSending chan struct{}
	output      chan outputMsg

	doneCheckLock sync.Mutex
	doneCheck     bool

	// Stats/infos
	readingStats *stats.Stats
	bar          *progressbar.ProgressBar
	fileSize     int64
	fname        string
	firstByte    bool
	spinner      *spinner.Spinner
}

// New creates a new sender session
func new(s internalSess.Session) *Session {
	spin := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	spin.Writer = os.Stderr
	spin.Suffix = " creating channel..."
	return &Session{
		sess:         s,
		initialized:  false,
		dataBuff:     make([]byte, senderBuffSize),
		stopSending:  make(chan struct{}, 1),
		output:       make(chan outputMsg, senderBuffSize*10),
		doneCheck:    false,
		readingStats: stats.New(),
		spinner:      spin,
	}
}

// New creates a new receiver session
func New() *Session {
	return new(internalSess.New(nil, nil))
}

// Config contains custom configuration for a session
type Config struct {
	common.Configuration
}

// NewWith createa a new sender Session with custom configuration
func NewWith(c Config) *Session {
	return new(internalSess.New(c.SDPProvider, c.SDPOutput))
}

func (s *Session) CreateConnection() (err error) {
	return s.sess.CreateConnection(s.onConnectionStateChange())
}

func (s *Session) CreateOffer() (string, error) {
	return s.sess.CreateOffer()
}

func (s *Session) SetSDP(sdp string) error {
	return s.sess.SetSDP(sdp)
}

func (s *Session) TransferFile(pathToFile string) {
	s.readFile(pathToFile)
	s.sess.OnCompletion()
}

// SDPProvider returns the underlying SDPProvider
func (s *Session) SDPProvider() io.Reader {
	return s.sess.SDPProvider()
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
// 	if err := s.createDataChannel(); err != nil {
// 		log.Errorln(err)
// 		return err
// 	}
// 	if err := s.sess.CreateOffer(); err != nil {
// 		log.Errorln(err)
// 		return err
// 	}

// 	s.initialized = true
// 	return nil
// }

// // Start the connection and the file transfer
// func (s *Session) Start() error {
// 	if err := s.Initialize(); err != nil {
// 		return err
// 	}
// 	go s.readFile()
// 	if err := s.sess.ReadSDP(); err != nil {
// 		log.Errorln(err)
// 		return err
// 	}
// 	<-s.sess.Done
// 	s.sess.OnCompletion()
// 	return nil
// }

func (s *Session) CreateDataChannel() error {
	s.spinner.Start()
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

func (s *Session) readFile(pathToFile string) error {
	f, err := os.Open(pathToFile)
	if err != nil {
		log.Error(err)
		return err
	}
	stat, _ := f.Stat()
	s.fileSize = stat.Size()
	s.fname = fmt.Sprintf("%12s", stat.Name())
	s.firstByte = true
	log.Debugf("Starting to read data from '%s'", pathToFile)
	s.readingStats.Start()
	defer func() {
		f.Close()
		s.readingStats.Pause()
		log.Debugf("Stopped reading data...")
		close(s.output)
	}()
	pos := uint64(0)
	for {
		// Read file
		s.dataBuff = s.dataBuff[:cap(s.dataBuff)]
		n, err := f.Read(s.dataBuff)
		if err != nil {
			if err == io.EOF {
				s.readingStats.Stop()
				log.Debugf("Got EOF after %v bytes!\n", s.readingStats.Bytes())
				return nil
			}
			log.Errorf("Read Error: %v\n", err)
			return err
		}
		s.dataBuff = s.dataBuff[:n]
		s.readingStats.AddBytes(uint64(n))

		posByte := make([]byte, 8)
		binary.LittleEndian.PutUint64(posByte, pos)

		buff := append([]byte(nil), posByte...)
		buff = append(buff, s.dataBuff...)
		buff = compress.Compress(buff)
		buff = crypt.EncryptToBytes(buff, []byte{1, 2, 3, 4})
		s.output <- outputMsg{
			n: n,
			// Make a copy of the buffer
			buff: buff,
		}
		pos += uint64(n)
	}
	return nil
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

		// currentSpeed := s.sess.NetworkStats.Bandwidth()
		// log.Debugf("Transferring at %.2f MB/s\r", currentSpeed)

		for len(s.msgToBeSent) != 0 {
			cur := s.msgToBeSent[0]

			if err := s.dataChannel.Send(cur.buff); err != nil {
				log.Errorf("Error, cannot send to client: %v\n", err)
				return
			}
			s.sess.NetworkStats.AddBytes(uint64(cur.n))
			if s.firstByte {
				s.firstByte = false
				s.spinner.Stop()
				s.bar = progressbar.NewOptions64(
					s.fileSize,
					progressbar.OptionOnCompletion(func() {
						fmt.Println(" ✔️")
					}),
					progressbar.OptionSetWidth(8),
					progressbar.OptionSetDescription(s.fname),
					progressbar.OptionSetRenderBlankState(true),
					progressbar.OptionSetBytes64(s.fileSize),
					progressbar.OptionSetWriter(os.Stderr),
					progressbar.OptionThrottle(100*time.Millisecond),
				)
			}
			s.bar.Add(cur.n)
			s.msgToBeSent = s.msgToBeSent[1:]
		}
	}
}

func (s *Session) writeToNetwork() {
	// Set callback, as transfer may be paused
	s.dataChannel.OnBufferedAmountLow(s.onBufferedAmountLow())
	<-s.stopSending
	s.dataChannel.OnBufferedAmountLow(nil)
	log.Debugf("Pausing network I/O... (remaining at least %v packets)\n", len(s.output))
	s.sess.NetworkStats.Pause()
}

func (s *Session) StopSending() {
	log.Debug("StopSending() triggered")
	s.stopSending <- struct{}{}
}

func (s *Session) onConnectionStateChange() func(connectionState webrtc.ICEConnectionState) {
	return func(connectionState webrtc.ICEConnectionState) {
		log.Debugf("ICE Connection State has changed: %s\n", connectionState.String())
		if connectionState == webrtc.ICEConnectionStateDisconnected {
			s.StopSending()
		}
	}
}

func (s *Session) onOpenHandler() func() {
	return func() {
		s.sess.NetworkStats.Start()

		log.Debugf("Starting to send data...")
		defer log.Debugf("Stopped sending data...")

		s.writeToNetwork()
	}
}

func (s *Session) onCloseHandler() func() {
	return func() {
		s.close(true)
	}
}

func (s *Session) close(calledFromCloseHandler bool) {
	if !calledFromCloseHandler {
		s.dataChannel.Close()
	}

	// Sometime, onCloseHandler is not invoked, so it's a work-around
	s.doneCheckLock.Lock()
	if s.doneCheck {
		s.doneCheckLock.Unlock()
		return
	}
	s.doneCheck = true
	s.doneCheckLock.Unlock()
	s.dumpStats()
	close(s.sess.Done)
}

func (s *Session) dumpStats() {
	log.Debugf(`Disk   : %s, Network: %s`, s.readingStats.String(), s.sess.NetworkStats.String())
}
