package main

import (
	"fmt"
	"net"
	"strconv"
	"sync"
)

func runClient(connectionType string, codePhrase string) {
	var wg sync.WaitGroup
	wg.Add(numberConnections)
	for id := 0; id < numberConnections; id++ {
		go func(id int) {
			defer wg.Done()
			port := strconv.Itoa(27001 + id)
			connection, err := net.Dial("tcp", "localhost:"+port)
			if err != nil {
				panic(err)
			}
			defer connection.Close()

			message := receiveMessage(connection)
			fmt.Println(message)
			sendMessage(connectionType+"."+codePhrase, connection)
			if connectionType == "s" {
				message = receiveMessage(connection)
				fmt.Println(message)
			}

		}(id)
	}
	wg.Wait()
}
