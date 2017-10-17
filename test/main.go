package main

import (
	"flag"
	"fmt"

	log "github.com/sirupsen/logrus"
)

const BUFFERSIZE = 1024
const numberConnections = 1

// Build flags
var server, file string

// Global varaibles
var serverAddress, fileName, codePhraseFlag, connectionTypeFlag string
var runAsRelay bool

func main() {
	flag.BoolVar(&runAsRelay, "relay", false, "run as relay")
	flag.StringVar(&serverAddress, "server", "", "(run as client) server address to connect to")
	flag.StringVar(&fileName, "file", "", "(run as server) file to serve")
	flag.StringVar(&codePhraseFlag, "code", "", "(run as server) file to serve")
	flag.StringVar(&connectionTypeFlag, "type", "", "(run as server) file to serve")
	flag.Parse()
	// Check build flags too, which take precedent
	if server != "" {
		serverAddress = server
	}
	if file != "" {
		fileName = file
	}
	if runAsRelay {
		runServer()
	} else if len(serverAddress) != 0 {
		runClient(connectionTypeFlag, codePhraseFlag)
	} else {
		fmt.Println("You must specify either -file (for running as a server) or -server (for running as a client)")
	}
}

func init() {
	// Log as JSON instead of the default ASCII formatter.
	// log.SetFormatter(&log.JSONFormatter{})
	log.SetFormatter(&log.TextFormatter{})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	// log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	log.SetLevel(log.DebugLevel)
}
