package main
//go:generate go run src/install/updateversion.go

import (
	"fmt"

	"github.com/schollz/croc/v6/src/cli"
)

func main() {
	if err := cli.Run(); err != nil {
		fmt.Println(err)
	}
}
