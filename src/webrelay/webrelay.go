package webrelay

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/schollz/croc/v6/src/comm"
	"github.com/schollz/croc/v6/src/models"
	"github.com/schollz/croc/v6/src/tcp"
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

type Bundle struct {
	Message       string `json:"m,omitempty"`
	PayloadString string `json:"ps,omitempty"`
	PayloadBytes  []byte `json:"pb,omitempty"`
}

func receive(conn *websocket.Conn) (err error) {
	conn.WriteMessage(websocket.TextMessage, []byte("initiated"))

	var com *comm.Comm
	var banner, externalIP, connectPort string
	for {
		var message []byte
		_, message, err = conn.ReadMessage()
		if err != nil {
			log.Debug("read:", err)
			return
		}
		var bu Bundle
		errBundle := json.Unmarshal(message, &bu)
		if errBundle == nil {
			if bu.Message == "room" {
				com, banner, externalIP, err = tcp.ConnectToTCPServer(models.DEFAULT_RELAY, bu.PayloadString)
				if err != nil {
					log.Error(err)
					return
				}
				log.Debugf("comm: %+v", com)
				log.Debugf("banner: %+v", banner)
				log.Debugf("externalIP: %+v", externalIP)
				log.Debugf("err: %+v", err)
				err = com.Send([]byte("ips?"))
				if err != nil {
					log.Error(err)
					return
				}
				var data []byte
				data, err = com.Receive()
				if err != nil {
					log.Error(err)
					return
				}
				log.Debugf("ips data: %s", data)

				err = com.Send([]byte("handshake"))
				if err != nil {
					log.Error(err)
					return
				}
				connectPort = strings.Split(banner, ",")[0]
				log.Debugf("connecting on %s", connectPort)
				err = conn.WriteMessage(websocket.TextMessage, []byte("initpake"))
				if err != nil {
					log.Error(err)
					return
				}
			} else if bu.Message == "initpake" {
				log.Debug("got init pake data")

			}

		}
	}
	return
}
