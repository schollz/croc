package main

import (
	"flag"

	"github.com/schollz/croc/v5/src/croc"
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
	// 	fmt.Println(bytes.Equal(buffer[:bytesread], emptyBuffer[:bytesread]))
	// }
	var sender bool
	flag.BoolVar(&sender, "sender", false, "sender")
	flag.Parse()
	c, err := croc.New(sender, "foo")
	if err != nil {
		panic(err)
	}
	if sender {
		err = c.Send(croc.TransferOptions{
			// PathToFile: "../wskeystore/README.md",
			// PathToFile:       "./src/croc/croc.go",
			// PathToFiles: []string{"C:\\Users\\zacks\\go\\src\\github.com\\schollz\\croc\\src\\croc\\croc.go", "croc.exe"},
			PathToFiles:      []string{"100mb.file"},
			KeepPathInRemote: false,
		})
	} else {
		err = c.Receive()
	}
	if err != nil {
		panic(err)
	}
}
