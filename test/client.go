package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

func runClient() {
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

			var messageByte []byte
			var message string
			messageByte = make([]byte, 64)
			connection.Read(messageByte)
			message = strings.Replace(string(messageByte), ":", "", -1)
			fmt.Println(message)
			message = fillString("r.1-2-3", 64)
			connection.Write([]byte(message))

		}(id)
	}
	wg.Wait()
}
