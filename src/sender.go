package croc

import (
	"net/url"
	"os"
	"os/signal"
	"time"

	log "github.com/cihub/seelog"
	"github.com/gorilla/websocket"
)

func (c *Croc) send(fname string) (err error) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: "localhost:8003", Path: "/"}
	log.Debugf("connecting to %s", u.String())

	ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Error("dial:", err)
		return
	}
	defer ws.Close()

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			var cd channelData
			err := ws.ReadJSON(&cd)
			if err != nil {
				log.Debugf("sender read error:", err)
				return
			}
			log.Debugf("recv: %+v", cd)
		}
	}()

	// initialize
	err = ws.WriteJSON(payload{
		Open: true,
	})
	if err != nil {
		log.Errorf("problem opening: %s", err.Error())
		return
	}

	for {
		select {
		case <-done:
			return
		case <-interrupt:
			log.Debugf("interrupt")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			errWrite := ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if errWrite != nil {
				log.Debugf("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
	return
}
