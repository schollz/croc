package main

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pion/webrtc/v2"
	log "github.com/schollz/logger"
)

const (
	bufferedAmountLowThreshold uint64 = 512 * 1024  // 512 KB
	maxBufferedAmount          uint64 = 1024 * 1024 // 1 MB
	maxPacketSize              uint64 = 65535
)

func setRemoteDescription(pc *webrtc.PeerConnection, sdp []byte) (err error) {
	log.Debug("unmarshaling remote description")
	var desc webrtc.SessionDescription
	err = json.Unmarshal(sdp, &desc)
	if err != nil {
		log.Error(err)
		return
	}
	log.Debug("apply the desc as the remote description")
	err = pc.SetRemoteDescription(desc)
	if err != nil {
		log.Error(err)
	}
	return
}

func createOfferer() (pc *webrtc.PeerConnection, err error) {
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
			buf := make([]byte, maxPacketSize)
			rand.Read(buf)
			its++
			if its == 30000000000 {
				buf = []byte{1, 2, 3}
			}
			err2 := sendData(buf)
			if err2 != nil {
				return
			}
			time.Sleep(1 * time.Second)
			if its == 3000000000 {
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
		fmt.Printf("got message: %x\n", md5.Sum(dcMsg.Data))
	})

	return pc, nil
}

//func main(this js.Value, inputs []js.Value) interface{} {
func main() {
	log.SetLevel("debug")
	log.Debugf("running with input")

	log.Debug("creating offer")
	answerPC, err := createOfferer()
	if err != nil {
		log.Error(err)
	}

	log.Debug("decoding")
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(offerstring))
	if err != nil {
		log.Error(err)
	}

	log.Debugf("setting remote description: %s", b)
	err = setRemoteDescription(answerPC, b)
	if err != nil {
		log.Error(err)
	}

	log.Debug("creating answer")
	answer, err := answerPC.CreateAnswer(nil)
	if err != nil {
		log.Error(err)
	}

	log.Debug("setting local description")
	err = answerPC.SetLocalDescription(answer)
	if err != nil {
		log.Error(err)
	}

	log.Debug("marshaling answer")
	desc2, err := json.Marshal(answer)
	if err != nil {
		log.Error(err)
	}

	log.Debug("copy and paste the following output into src/webrtc/answer.json:")
	fmt.Println(" ")
	fmt.Println(string(desc2))
	fmt.Println(" ")

	select {}
}
