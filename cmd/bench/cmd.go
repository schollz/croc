package bench

import (
	"github.com/schollz/croc/v5/pkg/session/bench"
	"github.com/schollz/croc/v5/pkg/session/common"
	log "github.com/sirupsen/logrus"
	"gopkg.in/urfave/cli.v1"
)

func handler(c *cli.Context) error {
	isMaster := c.Bool("master")

	sess := bench.NewWith(bench.Config{
		Master: isMaster,
		Configuration: common.Configuration{
			OnCompletion: func() {
			},
		},
	})
	return sess.Start()
}

// New creates the command
func New() cli.Command {
	log.Traceln("Installing 'bench' command")
	return cli.Command{
		Name:    "bench",
		Aliases: []string{"b"},
		Usage:   "Benchmark the connexion",
		Action:  handler,
		Flags: []cli.Flag{
			cli.BoolFlag{
				Name:  "master, m",
				Usage: "Is creating the SDP offer?",
			},
		},
	}
}
