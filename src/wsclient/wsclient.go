package wsclient

import (
	"net/url"

	"github.com/gorilla/websocket"
	log "github.com/schollz/logger"
)

func Connect() (err error) {

	u := url.URL{Scheme: "ws", Host: "localhost", Path: "/ws/test1"}
	log.Debugf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Error("dial:", err)
		return
	}
	defer c.Close()

	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			log.Printf("recv: %s", message)
		}
	}()
}
