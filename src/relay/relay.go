package relay

import (
	"fmt"
	"net/http"

	log "github.com/cihub/seelog"
	"github.com/schollz/croc/src/logger"
)

var DebugLevel string

// Run is the async operation for running a server
func Run(port string) (err error) {
	logger.SetLogLevel(DebugLevel)

	go h.run()
	log.Debug("running relay on " + port)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(w, r)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok")
	})
	err = http.ListenAndServe(":"+port, nil)
	return
}
