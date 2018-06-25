package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/mr-tron/base58/base58"
)

func checkSum(b []byte) []byte {
	sh1, sh2 := sha256.New(), sha256.New()
	sh1.Write(b)
	sh2.Write(sh1.Sum(nil))
	return sh2.Sum(nil)
}

func main() {
	var (
		err error
		bin []byte

		help     = flag.Bool("h", false, "display this message")
		lnBreak  = flag.Int("b", 76, "break encoded string into num character lines. Use 0 to disable line wrapping")
		input    = flag.String("i", "-", `input file (use: "-" for stdin)`)
		output   = flag.String("o", "-", `output file (use: "-" for stdout)`)
		decode   = flag.Bool("d", false, `decode input`)
		check    = flag.Bool("k", false, `use sha256 check`)
		useError = flag.Bool("e", false, `write error to stderr`)
	)

	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	fin, fout := os.Stdin, os.Stdout
	if *input != "-" {
		if fin, err = os.Open(*input); err != nil {
			fmt.Fprintf(os.Stderr, "input file err: %v\n", err)
			os.Exit(1)
		}
	}

	if *output != "-" {
		if fout, err = os.Create(*output); err != nil {
			fmt.Fprintf(os.Stderr, "output file err: %v\n", err)
			os.Exit(1)
		}
	}

	if bin, err = ioutil.ReadAll(fin); err != nil {
		fmt.Fprintf(os.Stderr, "read input err: %v\n", err)
		os.Exit(1)
	}

	if *decode {
		decoded, err := base58.FastBase58Decoding(string(bin))
		if err != nil {
			fmt.Fprintf(os.Stderr, "decode input err: %v\n", err)
			os.Exit(1)
		}

		var checkResult bool
		if *check {
			chk := len(decoded) - 4
			decodedCk := decoded[chk:]
			decoded = decoded[:chk]
			sum := checkSum(decoded)
			checkResult = hex.EncodeToString(sum[:4]) == hex.EncodeToString(decodedCk)
		}

		io.Copy(fout, bytes.NewReader(decoded))

		if *check && !checkResult {
			if *useError {
				fmt.Fprintf(os.Stderr, "%t", false)
			}
			os.Exit(3)
		}

		os.Exit(0)
	}

	if *check {
		sum := checkSum(bin)
		bin = append(bin, sum[:4]...)
	}

	encoded := base58.FastBase58Encoding(bin)

	if *lnBreak > 0 {
		lines := (len(encoded) / *lnBreak) + 1
		for i := 0; i < lines; i++ {
			start := i * *lnBreak
			end := start + *lnBreak
			if i == lines-1 {
				fmt.Fprintln(fout, encoded[start:])
				return
			}
			fmt.Fprintln(fout, encoded[start:end])
		}
	}
	fmt.Fprintln(fout, encoded)
}
