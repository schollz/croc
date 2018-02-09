// Disabling building of toml support in cases where golang is 1.0 or 1.1
// as the encoding library is not implemented or supported.

// +build go1.2

package altsrc

import (
	"flag"
	"io/ioutil"
	"os"
	"testing"

	"gopkg.in/urfave/cli.v1"
)

func TestCommandTomFileTest(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ioutil.WriteFile("current.toml", []byte("test = 15"), 0666)
	defer os.Remove("current.toml")
	test := []string{"test-cmd", "--load", "current.toml"}
	set.Parse(test)

	c := cli.NewContext(app, set, nil)

	command := &cli.Command{
		Name:        "test-cmd",
		Aliases:     []string{"tc"},
		Usage:       "this is for testing",
		Description: "testing",
		Action: func(c *cli.Context) error {
			val := c.Int("test")
			expect(t, val, 15)
			return nil
		},
		Flags: []cli.Flag{
			NewIntFlag(cli.IntFlag{Name: "test"}),
			cli.StringFlag{Name: "load"}},
	}
	command.Before = InitInputSourceWithContext(command.Flags, NewTomlSourceFromFlagFunc("load"))
	err := command.Run(c)

	expect(t, err, nil)
}

func TestCommandTomlFileTestGlobalEnvVarWins(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ioutil.WriteFile("current.toml", []byte("test = 15"), 0666)
	defer os.Remove("current.toml")

	os.Setenv("THE_TEST", "10")
	defer os.Setenv("THE_TEST", "")
	test := []string{"test-cmd", "--load", "current.toml"}
	set.Parse(test)

	c := cli.NewContext(app, set, nil)

	command := &cli.Command{
		Name:        "test-cmd",
		Aliases:     []string{"tc"},
		Usage:       "this is for testing",
		Description: "testing",
		Action: func(c *cli.Context) error {
			val := c.Int("test")
			expect(t, val, 10)
			return nil
		},
		Flags: []cli.Flag{
			NewIntFlag(cli.IntFlag{Name: "test", EnvVar: "THE_TEST"}),
			cli.StringFlag{Name: "load"}},
	}
	command.Before = InitInputSourceWithContext(command.Flags, NewTomlSourceFromFlagFunc("load"))

	err := command.Run(c)

	expect(t, err, nil)
}

func TestCommandTomlFileTestGlobalEnvVarWinsNested(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ioutil.WriteFile("current.toml", []byte("[top]\ntest = 15"), 0666)
	defer os.Remove("current.toml")

	os.Setenv("THE_TEST", "10")
	defer os.Setenv("THE_TEST", "")
	test := []string{"test-cmd", "--load", "current.toml"}
	set.Parse(test)

	c := cli.NewContext(app, set, nil)

	command := &cli.Command{
		Name:        "test-cmd",
		Aliases:     []string{"tc"},
		Usage:       "this is for testing",
		Description: "testing",
		Action: func(c *cli.Context) error {
			val := c.Int("top.test")
			expect(t, val, 10)
			return nil
		},
		Flags: []cli.Flag{
			NewIntFlag(cli.IntFlag{Name: "top.test", EnvVar: "THE_TEST"}),
			cli.StringFlag{Name: "load"}},
	}
	command.Before = InitInputSourceWithContext(command.Flags, NewTomlSourceFromFlagFunc("load"))

	err := command.Run(c)

	expect(t, err, nil)
}

func TestCommandTomlFileTestSpecifiedFlagWins(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ioutil.WriteFile("current.toml", []byte("test = 15"), 0666)
	defer os.Remove("current.toml")

	test := []string{"test-cmd", "--load", "current.toml", "--test", "7"}
	set.Parse(test)

	c := cli.NewContext(app, set, nil)

	command := &cli.Command{
		Name:        "test-cmd",
		Aliases:     []string{"tc"},
		Usage:       "this is for testing",
		Description: "testing",
		Action: func(c *cli.Context) error {
			val := c.Int("test")
			expect(t, val, 7)
			return nil
		},
		Flags: []cli.Flag{
			NewIntFlag(cli.IntFlag{Name: "test"}),
			cli.StringFlag{Name: "load"}},
	}
	command.Before = InitInputSourceWithContext(command.Flags, NewTomlSourceFromFlagFunc("load"))

	err := command.Run(c)

	expect(t, err, nil)
}

func TestCommandTomlFileTestSpecifiedFlagWinsNested(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ioutil.WriteFile("current.toml", []byte(`[top]
  test = 15`), 0666)
	defer os.Remove("current.toml")

	test := []string{"test-cmd", "--load", "current.toml", "--top.test", "7"}
	set.Parse(test)

	c := cli.NewContext(app, set, nil)

	command := &cli.Command{
		Name:        "test-cmd",
		Aliases:     []string{"tc"},
		Usage:       "this is for testing",
		Description: "testing",
		Action: func(c *cli.Context) error {
			val := c.Int("top.test")
			expect(t, val, 7)
			return nil
		},
		Flags: []cli.Flag{
			NewIntFlag(cli.IntFlag{Name: "top.test"}),
			cli.StringFlag{Name: "load"}},
	}
	command.Before = InitInputSourceWithContext(command.Flags, NewTomlSourceFromFlagFunc("load"))

	err := command.Run(c)

	expect(t, err, nil)
}

func TestCommandTomlFileTestDefaultValueFileWins(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ioutil.WriteFile("current.toml", []byte("test = 15"), 0666)
	defer os.Remove("current.toml")

	test := []string{"test-cmd", "--load", "current.toml"}
	set.Parse(test)

	c := cli.NewContext(app, set, nil)

	command := &cli.Command{
		Name:        "test-cmd",
		Aliases:     []string{"tc"},
		Usage:       "this is for testing",
		Description: "testing",
		Action: func(c *cli.Context) error {
			val := c.Int("test")
			expect(t, val, 15)
			return nil
		},
		Flags: []cli.Flag{
			NewIntFlag(cli.IntFlag{Name: "test", Value: 7}),
			cli.StringFlag{Name: "load"}},
	}
	command.Before = InitInputSourceWithContext(command.Flags, NewTomlSourceFromFlagFunc("load"))

	err := command.Run(c)

	expect(t, err, nil)
}

func TestCommandTomlFileTestDefaultValueFileWinsNested(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ioutil.WriteFile("current.toml", []byte("[top]\ntest = 15"), 0666)
	defer os.Remove("current.toml")

	test := []string{"test-cmd", "--load", "current.toml"}
	set.Parse(test)

	c := cli.NewContext(app, set, nil)

	command := &cli.Command{
		Name:        "test-cmd",
		Aliases:     []string{"tc"},
		Usage:       "this is for testing",
		Description: "testing",
		Action: func(c *cli.Context) error {
			val := c.Int("top.test")
			expect(t, val, 15)
			return nil
		},
		Flags: []cli.Flag{
			NewIntFlag(cli.IntFlag{Name: "top.test", Value: 7}),
			cli.StringFlag{Name: "load"}},
	}
	command.Before = InitInputSourceWithContext(command.Flags, NewTomlSourceFromFlagFunc("load"))

	err := command.Run(c)

	expect(t, err, nil)
}

func TestCommandTomlFileFlagHasDefaultGlobalEnvTomlSetGlobalEnvWins(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ioutil.WriteFile("current.toml", []byte("test = 15"), 0666)
	defer os.Remove("current.toml")

	os.Setenv("THE_TEST", "11")
	defer os.Setenv("THE_TEST", "")

	test := []string{"test-cmd", "--load", "current.toml"}
	set.Parse(test)

	c := cli.NewContext(app, set, nil)

	command := &cli.Command{
		Name:        "test-cmd",
		Aliases:     []string{"tc"},
		Usage:       "this is for testing",
		Description: "testing",
		Action: func(c *cli.Context) error {
			val := c.Int("test")
			expect(t, val, 11)
			return nil
		},
		Flags: []cli.Flag{
			NewIntFlag(cli.IntFlag{Name: "test", Value: 7, EnvVar: "THE_TEST"}),
			cli.StringFlag{Name: "load"}},
	}
	command.Before = InitInputSourceWithContext(command.Flags, NewTomlSourceFromFlagFunc("load"))
	err := command.Run(c)

	expect(t, err, nil)
}

func TestCommandTomlFileFlagHasDefaultGlobalEnvTomlSetGlobalEnvWinsNested(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ioutil.WriteFile("current.toml", []byte("[top]\ntest = 15"), 0666)
	defer os.Remove("current.toml")

	os.Setenv("THE_TEST", "11")
	defer os.Setenv("THE_TEST", "")

	test := []string{"test-cmd", "--load", "current.toml"}
	set.Parse(test)

	c := cli.NewContext(app, set, nil)

	command := &cli.Command{
		Name:        "test-cmd",
		Aliases:     []string{"tc"},
		Usage:       "this is for testing",
		Description: "testing",
		Action: func(c *cli.Context) error {
			val := c.Int("top.test")
			expect(t, val, 11)
			return nil
		},
		Flags: []cli.Flag{
			NewIntFlag(cli.IntFlag{Name: "top.test", Value: 7, EnvVar: "THE_TEST"}),
			cli.StringFlag{Name: "load"}},
	}
	command.Before = InitInputSourceWithContext(command.Flags, NewTomlSourceFromFlagFunc("load"))
	err := command.Run(c)

	expect(t, err, nil)
}
