package termui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestShouldUseColor(t *testing.T) {
	tests := []struct {
		name         string
		noColor      string
		terminalName string
		isTerminal   bool
		want         bool
	}{
		{name: "interactive terminal", terminalName: "xterm-256color", isTerminal: true, want: true},
		{name: "redirected output", terminalName: "xterm-256color", isTerminal: false},
		{name: "NO_COLOR", noColor: "1", terminalName: "xterm-256color", isTerminal: true},
		{name: "dumb terminal", terminalName: "dumb", isTerminal: true},
		{name: "dumb terminal case insensitive", terminalName: " DUMB ", isTerminal: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := shouldUseColor(test.noColor, test.terminalName, test.isTerminal)
			if got != test.want {
				t.Fatalf("shouldUseColor(%q, %q, %t) = %t; want %t", test.noColor, test.terminalName, test.isTerminal, got, test.want)
			}
		})
	}
}

func TestOutputDisablesColorForPipe(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	output, enabled := Output(writer)
	if enabled {
		t.Fatal("color enabled for redirected output")
	}
	if output != writer {
		t.Fatalf("redirected writer = %T; want original *os.File", output)
	}

	output, enabled = Output(nil)
	if enabled || output != io.Discard {
		t.Fatalf("nil terminal output = (%T, %t); want (io.Discard, false)", output, enabled)
	}
}

func TestPromptChoices(t *testing.T) {
	const prompt = "Replace example.txt? (y/N) Receive these files? (Y/n)"

	if got := PromptChoices(prompt, false); got != prompt {
		t.Fatalf("plain prompt = %q; want %q", got, prompt)
	}

	got := PromptChoices(prompt, true)
	if strings.Count(got, Bold) != 2 {
		t.Fatalf("bold style count = %d; want 2", strings.Count(got, Bold))
	}
	if stripANSI(got) != prompt {
		t.Fatalf("stripped prompt = %q; want %q", stripANSI(got), prompt)
	}
}

func TestPlainRemovesTerminalStyles(t *testing.T) {
	styled := Filename("croc.txt", true) + " " + Success("done", true)
	if got := Plain(styled); got != "croc.txt done" {
		t.Fatalf("plain text = %q; want %q", got, "croc.txt done")
	}
}

func TestLoggerWriterNormalizesAndStylesLevels(t *testing.T) {
	const input = "\x1b[0;33m[warn]\t\x1b[0mwarning\n\x1b[0;31;1m[error]\t\x1b[0mfailure\n"

	t.Run("plain", func(t *testing.T) {
		var output bytes.Buffer
		writer := &loggerWriter{output: &output}
		if _, err := writer.Write([]byte(input)); err != nil {
			t.Fatalf("write log: %v", err)
		}
		want := "[warn]\twarning\n[error]\tfailure\n"
		if output.String() != want {
			t.Fatalf("plain log = %q; want %q", output.String(), want)
		}
	})

	t.Run("color", func(t *testing.T) {
		var output bytes.Buffer
		writer := &loggerWriter{output: &output, colorEnabled: true}
		if _, err := writer.Write([]byte(input)); err != nil {
			t.Fatalf("write log: %v", err)
		}
		got := output.String()
		if !strings.Contains(got, Yellow+"[warn]"+Reset) {
			t.Fatalf("warning level is not yellow: %q", got)
		}
		if !strings.Contains(got, Red+"[error]"+Reset) {
			t.Fatalf("error level is not red: %q", got)
		}
		if stripANSI(got) != "[warn]\twarning\n[error]\tfailure\n" {
			t.Fatalf("colored log changed text: %q", got)
		}
	})
}

func TestSemanticStylesUseGitLikePalette(t *testing.T) {
	tests := []struct {
		name  string
		got   string
		style string
	}{
		{name: "routine emphasis", got: Emphasis("Sending", true), style: Bold},
		{name: "filename", got: Filename("croc", true), style: Bold},
		{name: "secret", got: Secret("trio-door-handle-upside", true), style: Yellow},
		{name: "success", got: Success("Complete", true), style: Green},
		{name: "warning", got: Warning("Overwrite", true), style: Yellow},
		{name: "error", got: Error("Failed", true), style: Red},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !strings.HasPrefix(test.got, test.style) || !strings.HasSuffix(test.got, Reset) {
				t.Fatalf("styled text = %q; want prefix %q and reset", test.got, test.style)
			}
		})
	}
}

func stripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}
