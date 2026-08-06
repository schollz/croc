package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestDirectHelpRenderer(t *testing.T) {
	app := NewApp()
	app.Name = "croc"
	app.HelpName = "croc"
	app.Usage = "transfer files"
	app.Version = "1.2.3"
	app.Commands = []*Command{{
		Name:      "send",
		Usage:     "send files",
		ArgsUsage: "[files]",
		Flags: []Flag{
			&StringFlag{Name: "code", Usage: "transfer code"},
		},
	}}
	app.Flags = []Flag{&BoolFlag{Name: "quiet", Usage: "disable output"}}

	var output bytes.Buffer
	app.Writer = &output
	app.Setup()
	if err := ShowAppHelp(NewContext(app, nil, nil)); err != nil {
		t.Fatal(err)
	}

	help := output.String()
	for _, expected := range []string{
		"croc - transfer files",
		"VERSION:\n   1.2.3",
		"send files",
		"--quiet",
		"disable output",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("help output does not contain %q:\n%s", expected, help)
		}
	}
	if strings.Contains(help, "{{") {
		t.Fatalf("help output contains an unrendered template action:\n%s", help)
	}
}

func TestDirectFishCompletionRenderer(t *testing.T) {
	app := NewApp()
	app.Name = "croc"
	app.Commands = []*Command{{Name: "send", Usage: "send files"}}
	app.Flags = []Flag{&BoolFlag{Name: "quiet", Usage: "disable output"}}
	app.Setup()

	completion, err := app.ToFishCompletion()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"# croc fish shell completion",
		"if contains -- $i send help h",
		"-l quiet -d 'disable output'",
		"-a 'send' -d 'send files'",
	} {
		if !strings.Contains(completion, expected) {
			t.Fatalf("completion output does not contain %q:\n%s", expected, completion)
		}
	}
}
