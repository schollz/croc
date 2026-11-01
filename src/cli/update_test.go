package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateRegistrationCompatibility(t *testing.T) {
	config := t.TempDir()
	t.Setenv("CROC_CONFIG_DIR", config)
	app := newApp()
	app.Writer = &bytes.Buffer{}
	if err := app.RunContext(context.Background(), []string{"croc", "update", "--register-installer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(config, "install.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy registration wrote configuration: %v", err)
	}
}

func TestUpdateHelpExplainsHelper(t *testing.T) {
	for _, alias := range []string{"update", "upgrade"} {
		var output bytes.Buffer
		app := newApp()
		app.Writer = &output
		if err := app.RunContext(context.Background(), []string{"croc", alias, "--help"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "croc-update") {
			t.Fatal(output.String())
		}
		if strings.Contains(output.String(), "internal-install-version") {
			t.Fatal("internal operation appears in help")
		}
	}
}
