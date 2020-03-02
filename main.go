package main

//go:generate git tag -af v$VERSION -m "v$VERSION"
//go:generate go run src/install/updateversion.go
//go:generate git commit -am "bump $VERSION"
//go:generate git tag -af v$VERSION -m "v$VERSION"

import (
	"fmt"

	"github.com/schollz/croc/v8/src/cli"
)

func main() {
	// "github.com/pkg/profile"
	// defer profile.Start(profile.CPUProfile).Stop()
	if err := cli.Run(); err != nil {
		fmt.Println(err)
	}
}
