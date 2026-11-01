package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/schollz/croc/v11/src/version"
)

func TestVersionHelpAndArguments(t *testing.T) {
	for _, arg := range []string{"--version", "--help"} {
		var output bytes.Buffer
		if err := run(context.Background(), []string{arg}, &output); err != nil {
			t.Fatal(err)
		}
		if arg == "--version" && strings.TrimSpace(output.String()) != "croc-update version "+version.Value {
			t.Fatal(output.String())
		}
		if strings.Contains(output.String(), "internal-install-version") {
			t.Fatal("internal operation exposed in help")
		}
	}
	if err := run(context.Background(), []string{"unexpected"}, &bytes.Buffer{}); err == nil {
		t.Fatal("unexpected argument accepted")
	}
}
