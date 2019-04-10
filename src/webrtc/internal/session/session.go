package session

import (
	"io"
	"log"
	"os"

	"github.com/pion/webrtc/v2"
	"github.com/schollz/croc/v5/src/webrtc/pkg/stats"
	"github.com/schollz/croc/v5/src/webrtc/pkg/utils"
)

// SDPProvider returns the SDP input
func (s *Session) SDPProvider() io.Reader {
	return s.sdpInput
}

// CompletionHandler to be called when transfer is done
type CompletionHandler func()

// Session contains common elements to perform send/receive
type Session struct {
	Done           chan struct{}
	NetworkStats   *stats.Stats
	sdpInput       io.Reader
	sdpOutput      io.Writer
	peerConnection *webrtc.PeerConnection
	onCompletion   CompletionHandler
}

// New creates a new Session
func New(sdpInput io.Reader, sdpOutput io.Writer) Session {
	log.Println("making new channel")
	if sdpInput == nil {
		sdpInput = os.Stdin
	}
	if sdpOutput == nil {
		sdpOutput = os.Stdout
	}
	return Session{
		sdpInput:     sdpInput,
		sdpOutput:    sdpOutput,
		Done:         make(chan struct{}),
		NetworkStats: stats.New(),
	}
}

// CreateConnection prepares a WebRTC connection
func (s *Session) CreateConnection(onConnectionStateChange func(connectionState webrtc.ICEConnectionState)) error {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return err
	}
	s.peerConnection = peerConnection
	peerConnection.OnICEConnectionStateChange(onConnectionStateChange)

	return nil
}

// SetSDP sets the SDP
func (s *Session) SetSDP(encoded string) error {
	var sdp webrtc.SessionDescription
	err := utils.Decode(encoded, &sdp)
	if err != nil {
		return err
	}
	return s.peerConnection.SetRemoteDescription(sdp)
}

// CreateDataChannel that will be used to send data
func (s *Session) CreateDataChannel(c *webrtc.DataChannelInit) (*webrtc.DataChannel, error) {
	return s.peerConnection.CreateDataChannel("data", c)
}

// OnDataChannel sets an OnDataChannel handler
func (s *Session) OnDataChannel(handler func(d *webrtc.DataChannel)) {
	s.peerConnection.OnDataChannel(handler)
}

// CreateAnswer set the local description and print the answer SDP
func (s *Session) CreateAnswer() (string, error) {
	// Create an answer
	answer, err := s.peerConnection.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	return s.createSessionDescription(answer)
}

// CreateOffer set the local description and print the offer SDP
func (s *Session) CreateOffer() (string, error) {
	// Create an offer
	answer, err := s.peerConnection.CreateOffer(nil)
	if err != nil {
		return "", err
	}
	return s.createSessionDescription(answer)
}

// createSessionDescription set the local description and print the SDP
func (s *Session) createSessionDescription(desc webrtc.SessionDescription) (string, error) {
	// Sets the LocalDescription, and starts our UDP listeners
	if err := s.peerConnection.SetLocalDescription(desc); err != nil {
		return "", err
	}
	desc.SDP = utils.StripSDP(desc.SDP)

	// Output the SDP in base64 so we can paste it in browser
	return utils.Encode(desc)
}

// OnCompletion is called when session ends
func (s *Session) OnCompletion() {
	if s.onCompletion != nil {
		s.onCompletion()
	}
}
