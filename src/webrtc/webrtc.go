package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
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

func createOfferer(finished chan<- error) (pc *webrtc.PeerConnection, err error) {
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
			// buf := make([]byte, maxPacketSize)
			buf := []byte(fmt.Sprintf("%d\n", its))
			// rand.Read(buf)
			its++
			if its == 3000000000 {
				buf = []byte{1, 2, 3}
				finished <- errors.New("done")
			}
			err2 := sendData(buf)
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
		fmt.Printf("got message: %x\n", md5.Sum(dcMsg.Data))
		if len(dcMsg.Data) < 100 {
			log.Debugf("msg: %s", string(dcMsg.Data))
		}
	})

	return pc, nil
}

func main() {
	log.SetLevel("debug")
	finished := make(chan error, 1)
	var sender bool
	flag.BoolVar(&sender, "sender", false, "set as sender")
	flag.Parse()
	log.SetLevel("debug")
	if sender {
		os.Remove("answer.json")
		os.Remove("offer.json")
		offerPC, err := createOfferer(finished)
		if err != nil {
			log.Error(err)
		}
		// Now, create an offer
		offer, err := offerPC.CreateOffer(nil)
		if err != nil {
			log.Error(err)
		}

		err = offerPC.SetLocalDescription(offer)
		if err != nil {
			log.Error(err)
		}

		desc, err := json.Marshal(offer)
		if err != nil {
			log.Error(err)
		}
		fmt.Println(base64.StdEncoding.EncodeToString(desc))
		err = ioutil.WriteFile("offer.json", desc, 0644)
		if err != nil {
			log.Error(err)
		}

		// wait for answer
		for {
			b, errFile := ioutil.ReadFile("answer.json")
			if errFile != nil {
				time.Sleep(3 * time.Second)
				continue
			}
			fmt.Println(string(b))
			fmt.Println(time.Now())
			err = setRemoteDescription(offerPC, b)
			if err != nil {
				log.Error(err)
				time.Sleep(3 * time.Second)
				continue
			}
			break
		}
		log.Debug("sender succeeded")
	} else {
		answerPC, err := createOfferer(finished)
		if err != nil {
			log.Error(err)
		}

		b, err := ioutil.ReadFile("offer.json")
		if err != nil {
			log.Error(err)
		}

		err = setRemoteDescription(answerPC, b)
		if err != nil {
			log.Error(err)
		}

		answer, err := answerPC.CreateAnswer(nil)
		if err != nil {
			log.Error(err)
		}
		err = answerPC.SetLocalDescription(answer)
		if err != nil {
			log.Error(err)
		}

		desc2, err := json.Marshal(answer)
		if err != nil {
			log.Error(err)
		}

		fmt.Println(string(desc2))
		err = ioutil.WriteFile("answer.json", desc2, 0644)
		if err != nil {
			log.Error(err)
		}
	}

	// Block forever
	log.Debug("blocking forever")
	err := <-finished
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println("finished gracefully")
	}
}
