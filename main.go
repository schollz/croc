package main

import (
	"fmt"

	"github.com/schollz/croc/v5/src/cli"
)

func main() {
	// f, _ := os.Create("test.1")
	// f.Truncate(8096)
	// f.Close()

	// file, _ := os.Open("test.1")
	// defer file.Close()

	// buffer := make([]byte, 4096)
	// emptyBuffer := make([]byte, 4096)
	// for {
	// 	bytesread, err := file.Read(buffer)
	// 	if err != nil {
	// 		break
	// 	}
	// 	log.Debugln(bytes.Equal(buffer[:bytesread], emptyBuffer[:bytesread]))
	// }
	// var sender bool
	// flag.BoolVar(&sender, "sender", false, "sender")
	// flag.Parse()
	// c, err := croc.New(sender, "foo")
	// if err != nil {
	// 	panic(err)
	// }
	// if sender {
	// 	err = c.Send(croc.TransferOptions{
	// 		// PathToFile: "../wskeystore/README.md",
	// 		// PathToFile:       "./src/croc/croc.go",
	// 		// PathToFiles: []string{"C:\\Users\\zacks\\go\\src\\github.com\\schollz\\croc\\src\\croc\\croc.go", "croc.exe"},
	// 		PathToFiles: []string{"croc2.exe", "croc3.exe"}, //,"croc2.exe", "croc3.exe"},
	// 		//PathToFiles:      []string{"README.md"}, //,"croc2.exe", "croc3.exe"},
	// 		KeepPathInRemote: false,
	// 	})
	// } else {
	// 	err = c.Receive()
	// }
	// if err != nil {
	// 	fmt.Println(err)
	// }
	if err := cli.Run(); err != nil {
		fmt.Println(err)
	}
}
