package cmd

import (
	"sort"

	"github.com/schollz/croc/cmd/bench"
	"github.com/schollz/croc/cmd/receive"
	"github.com/schollz/croc/cmd/send"
	log "github.com/sirupsen/logrus"
	"gopkg.in/urfave/cli.v1"
)

// Install all the commands
func Install(app *cli.App) {
	app.Commands = []cli.Command{
		send.New(),
		receive.New(),
		bench.New(),
	}
	log.Trace("Installed commands")

	sort.Sort(cli.CommandsByName(app.Commands))
}
