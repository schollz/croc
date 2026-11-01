package update

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsInUseExecutable(t *testing.T) {
	old := buildFixture(t, Croc, "11.5.0")
	latest := buildFixture(t, Croc, "11.6.0")
	dir := t.TempDir()
	target := writeFixture(t, dir, Croc, old)
	staged := filepath.Join(dir, "staged.exe")
	if err := os.WriteFile(staged, latest, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), target, "--hold")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { input.Close(); command.Wait() }()
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || strings.TrimSpace(line) != "holding" {
		t.Fatalf("%q %v", line, err)
	}
	if err := replaceFile(staged, target, true); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("expected an in-use error: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, old) {
		t.Fatal("in-use replacement changed original")
	}
}
