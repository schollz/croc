package croc

import (
	"crypto/elliptic"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v2"
	"github.com/schollz/croc/v7/src/compress"
	"github.com/schollz/croc/v7/src/crypt"
	log "github.com/schollz/logger"
	"github.com/schollz/pake/v2"
)

// Debug toggles debug mode
func Debug(debug bool) {
	if debug {
		log.SetLevel("debug")
	} else {
		log.SetLevel("warn")
	}
}

// Options specifies user specific options
type Options struct {
	IsSender     bool
	SharedSecret string
	Debug        bool
	RelayAddress string
	Stdout       bool
	NoPrompt     bool
	DisableLocal bool
	Ask          bool
}

// Client holds the state of the croc transfer
type Client struct {
	// connections
	ws  *websocket.Conn
	rtc *webrtc.PeerConnection

	// options
	Options Options

	// security
	Pake *pake.Pake
	Key  crypt.Encryption

	// steps involved in forming relationship
	Step1ChannelSecured bool
	IsOfferer           bool
}

// TransferOptions for sending
type TransferOptions struct {
	PathToFiles      []string
	KeepPathInRemote bool
}

type WebsocketMessage struct {
	Message string
	Payload []byte
}

func (c *Client) Bundle(payload interface{}) (p []byte, err error) {
	p, err = json.Marshal(payload)
	if err != nil {
		return
	}
	p = compress.Compress(p)
	p, err = c.Key.Encrypt(p)
	return
}

func (c *Client) Unbundle(msg []byte, payload interface{}) (err error) {
	b, err := c.Key.Decrypt(msg)
	if err != nil {
		return
	}
	b = compress.Decompress(b)
	err = json.Unmarshal(b, &payload)
	return
}

// SendWebsocketMessage communicates using base64
func (c *Client) SendWebsocketMessage(wsmsg WebsocketMessage, encrypt bool) (err error) {
	var b []byte
	if encrypt {
		b, err = c.Bundle(wsmsg)
	} else {
		b, err = json.Marshal(wsmsg)
		if err != nil {
			log.Error(err)
			return
		}
	}
	bd := base64.StdEncoding.EncodeToString(b)
	err = c.ws.WriteMessage(1, []byte(bd))
	return
}

// ReceiveWebsocketMessage communicates using base64
func (c *Client) ReceiveWebsocketMessage(decrypt bool) (wsmsg WebsocketMessage, err error) {
	_, msg, err := c.ws.ReadMessage()
	if err != nil {
		log.Error(err)
		return
	}
	b, err := base64.StdEncoding.DecodeString(string(msg))
	if decrypt {
		err = c.Unbundle(b, &wsmsg)
		if err != nil {
			log.Error(err)
			return
		}
	} else {
		err = json.Unmarshal(b, &wsmsg)
	}
	return
}

// New establishes a new connection for transferring files between two instances.
func New(ops Options) (c *Client, err error) {
	c = new(Client)

	// setup basic info
	c.Options = ops
	if c.Options.Debug {
		log.SetLevel("debug")
	} else {
		log.SetLevel("info")
	}

	// connect to relay and determine
	// whether it is receiver or offerer
	err = c.connectToRelay()
	if err != nil {
		return
	}

	// initialize pake
	if c.IsOfferer {
		c.Pake, err = pake.Init([]byte(c.Options.SharedSecret), 0, elliptic.P521(), 1*time.Microsecond)
	} else {
		c.Pake, err = pake.Init([]byte(c.Options.SharedSecret), 1, elliptic.P521(), 1*time.Microsecond)
	}
	if err != nil {
		return
	}

	if c.IsOfferer {
		// offerer sends the first pake
		c.SendWebsocketMessage(WebsocketMessage{
			Message: "pake",
			Payload: c.Pake.Bytes(),
		}, false)
	} else {
		// answerer receives the first pake
		err = c.getPAKE(true)
		if err != nil {
			log.Error(err)
			return
		}
	}

	// one more exchange to finish (offerer must send)
	err = c.getPAKE(c.IsOfferer)
	if err != nil {
		log.Error(err)
		return
	}
	log.Debug(c.Pake.SessionKey())

	// generate the session key for encryption
	pakeSessionKey, err := c.Pake.SessionKey()
	if err != nil {
		log.Error(err)
		return
	}
	c.Key, err = crypt.New(pakeSessionKey, []byte(c.Options.SharedSecret))
	if err != nil {
		log.Error(err)
		return
	}

	// create webrtc connection
	finished := make(chan error, 1)
	c.rtc, err = c.CreateOfferer(finished)
	if err != nil {
		log.Error(err)
	}

	offer, err := c.rtc.CreateOffer(nil)
	if err != nil {
		log.Error(err)
		return
	}
	if c.IsOfferer {
		// Now, create an offer
		err = c.rtc.SetLocalDescription(offer)
		if err != nil {
			log.Error(err)
			return
		}

		// bundle it and send it over
		var offerJSON []byte
		offerJSON, err = json.Marshal(offer)
		if err != nil {
			log.Error(err)
		}
		err = c.SendWebsocketMessage(
			WebsocketMessage{Message: "offer", Payload: offerJSON},
			true,
		)
		if err != nil {
			log.Error(err)
			return
		}

		// wait for the answer
		var wsmsg WebsocketMessage
		wsmsg, err = c.ReceiveWebsocketMessage(true)

		err = setRemoteDescription(c.rtc, wsmsg.Payload)
		if err != nil {
			log.Error(err)
			return
		}
	} else {
		// wait for the offer
		var wsmsg WebsocketMessage
		wsmsg, err = c.ReceiveWebsocketMessage(true)

		err = setRemoteDescription(c.rtc, wsmsg.Payload)
		if err != nil {
			log.Error(err)
			return
		}

		var answer webrtc.SessionDescription
		answer, err = c.rtc.CreateAnswer(nil)
		if err != nil {
			log.Error(err)
			return
		}
		err = c.rtc.SetLocalDescription(answer)
		if err != nil {
			log.Error(err)
			return
		}

		// bundle it and send it over
		var answerJSON []byte
		answerJSON, err = json.Marshal(answer)
		if err != nil {
			log.Error(err)
		}
		err = c.SendWebsocketMessage(
			WebsocketMessage{Message: "answer", Payload: answerJSON},
			true,
		)
		if err != nil {
			log.Error(err)
			return
		}

	}

	err = <-finished
	return
}

func (c *Client) getPAKE(keepSending bool) (err error) {
	// answerer receives the first pake
	p, err := c.ReceiveWebsocketMessage(false)
	if err != nil {
		log.Error(err)
		return
	}
	log.Debugf("getPake got: %+v", p)
	err = c.Pake.Update(p.Payload)
	if err != nil {
		log.Error(err)
		return
	}
	if keepSending {
		//  sends back PAKE bytes
		err = c.SendWebsocketMessage(WebsocketMessage{
			Message: "pake",
			Payload: c.Pake.Bytes(),
		}, false)
	}
	return
}

// Send will send the specified file
func (c *Client) Send(options TransferOptions) (err error) {
	return
}

// Receiver will receive the file
func (c *Client) Receive() (err error) {
	return
}

func (c *Client) connectToRelay() (err error) {
	// connect to relay
	websocketURL := c.Options.RelayAddress + "/test1"
	log.Debugf("dialing %s", websocketURL)
	c.ws, _, err = websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		log.Error("dial:", err)
		return
	}

	log.Debugf("connected and sending first message")
	c.SendWebsocketMessage(WebsocketMessage{
		Message: "offerer",
	}, false)
	wsmsg, err := c.ReceiveWebsocketMessage(false)
	if err != nil {
		log.Debug("read:", err)
		return
	}
	log.Debugf("recv: %s", wsmsg)
	if wsmsg.Message == "offerer" {
		c.IsOfferer = true
		c.ws.WriteJSON(WebsocketMessage{
			Message: "answerer",
		})
	} else if wsmsg.Message == "answerer" {
		c.IsOfferer = false
	} else {
		err = fmt.Errorf("got bad message: %+v", wsmsg)
		log.Error(err)
	}
	return
}

const (
	bufferedAmountLowThreshold uint64 = 512 * 1024  // 512 KB
	maxBufferedAmount          uint64 = 1024 * 1024 // 1 MB
	maxPacketSize              uint64 = 65535
)

func setRemoteDescription(pc *webrtc.PeerConnection, sdp []byte) (err error) {
	log.Debug("setting remote description")
	var desc webrtc.SessionDescription
	err = json.Unmarshal(sdp, &desc)
	if err != nil {
		log.Error(err)
		return
	}

	log.Debug("applying remote description")
	// Apply the desc as the remote description
	err = pc.SetRemoteDescription(desc)
	if err != nil {
		log.Error(err)
	}
	return
}

func (c *Client) CreateOfferer(finished chan<- error) (pc *webrtc.PeerConnection, err error) {
	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	}

	// Create a new PeerConnection
	pc, err = webrtc.NewPeerConnection(config)
	if err != nil {
		log.Error(err)
		return
	}

	ordered := false
	maxRetransmits := uint16(0)
	options := &webrtc.DataChannelInit{
		Ordered:        &ordered,
		MaxRetransmits: &maxRetransmits,
	}

	sendMoreCh := make(chan struct{})

	// Create a datachannel with label 'data'
	dc, err := pc.CreateDataChannel("data", options)
	if err != nil {
		log.Error(err)
		return
	}

	// Register channel opening handling
	sendData := func(buf []byte) error {
		fmt.Printf("sent message: %x\n", md5.Sum(buf))
		err := dc.Send(buf)
		if err != nil {
			return err
		}
		if dc.BufferedAmount()+uint64(len(buf)) > maxBufferedAmount {
			// wait until the bufferedAmount becomes lower than the threshold
			<-sendMoreCh
		}
		return nil
	}

	dc.OnOpen(func() {
		fmt.Println(time.Now())
		log.Debugf("OnOpen: %s-%d. Start sending a series of 1024-byte packets as fast as it can\n", dc.Label(), dc.ID())
		its := 0
		for {
			its++

			msg, _ := c.Bundle(WebsocketMessage{
				Message: fmt.Sprintf("%d", its),
			})
			err2 := sendData(msg)
			if err2 != nil {
				finished <- err2
				return
			}
			time.Sleep(1 * time.Second)
			if its == 30000000 {
				break
			}
		}
	})

	// Set bufferedAmountLowThreshold so that we can get notified when
	// we can send more
	dc.SetBufferedAmountLowThreshold(bufferedAmountLowThreshold)

	// This callback is made when the current bufferedAmount becomes lower than the threadshold
	dc.OnBufferedAmountLow(func() {
		sendMoreCh <- struct{}{}
	})

	// Register the OnMessage to handle incoming messages
	dc.OnMessage(func(dcMsg webrtc.DataChannelMessage) {
		var wsmsg WebsocketMessage
		err := c.Unbundle(dcMsg.Data, &wsmsg)
		if err == nil {
			log.Debugf("wsmsg: %+v", wsmsg)
		} else {
			log.Error(err)
		}
	})

	return pc, nil
}
