package webrelay

import (
	"bytes"
	"net/http"

	"github.com/gorilla/websocket"
	log "github.com/schollz/logger"
)

func Run(debugString, port string) (err error) {
	log.SetLevel(debugString)
	http.HandleFunc("/ws", handlews)
	http.Handle("/", http.FileServer(http.Dir("html")))
	log.Infof("running on port %s", port)
	return http.ListenAndServe(":"+port, nil)
}

var upgrader = websocket.Upgrader{} // use default options

func handlews(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Debug("upgrade:", err)
		return
	}
	log.Debugf("connected: %+v", c.RemoteAddr())
	defer c.Close()

	_, message, err := c.ReadMessage()
	if err != nil {
		log.Debug("read:", err)
		return
	}
	log.Debugf("recv: %s", message)
	if bytes.Equal(message, []byte("receive")) {
		// start receiving
		log.Debug("initiating reciever")
		err = receive(c)
		if err != nil {
			log.Error(err)
		}
	}
	return
}

func receive(c *websocket.Conn) (err error) {
	c.WriteMessage(websocket.TextMessage, []byte("ok"))
	for {
		var message []byte
		_, message, err = c.ReadMessage()
		if err != nil {
			log.Debug("read:", err)
			return
		}
		log.Debugf("recv: %s", message)

	}
	return
}
