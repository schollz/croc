package relay

import (
	"context"
	"fmt"
	"net/http"
	"time"

	log "github.com/cihub/seelog"
	"github.com/schollz/croc/src/logger"
	"github.com/schollz/croc/src/tcp"
)

var DebugLevel string
var stop bool

func Stop() {
	log.Debug("got stop signal")
	stop = true
}

// Run is the async operation for running a server
func Run(port string, tcpPorts []string) (err error) {
	logger.SetLogLevel(DebugLevel)

	if len(tcpPorts) > 0 {
		for _, tcpPort := range tcpPorts {
			go tcp.Run(DebugLevel, tcpPort)
		}
	}

	go h.run()
	log.Debug("running relay on " + port)
	m := http.NewServeMux()
	s := http.Server{Addr: ":" + port, Handler: m}
	m.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(w, r)
	})
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok")
	})
	go func() {
		for {
			if stop {
				s.Shutdown(context.Background())
				log.Debug("stopping http server")
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	s.ListenAndServe()
	log.Debug("finished")
	return
}
