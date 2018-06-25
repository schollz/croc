package main

import (
	"flag"
	"io"
	"log"
	"os"
	"path"

	"bitbucket.org/dchapes/mnemonicode"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix(path.Base(os.Args[0]) + ": ")
	hexFlag := flag.Bool("x", false, "hex output")
	verboseFlag := flag.Bool("v", false, "verbose")
	flag.Parse()
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}

	output := io.WriteCloser(os.Stdout)
	if *hexFlag {
		output = hexoutput(output)
	}

	var n int64
	var err error
	if true {
		dec := mnemonicode.NewDecoder(os.Stdin)
		n, err = io.Copy(output, dec)
	} else {
		w := mnemonicode.NewDecodeWriter(output)
		n, err = io.Copy(w, os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		err = w.Close()
	}
	if err != nil {
		log.Fatal(err)
	}
	if *verboseFlag {
		log.Println("bytes decoded:", n)
	}
	if err = output.Close(); err != nil {
		log.Fatal(err)
	}
}
