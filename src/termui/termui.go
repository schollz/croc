// Package termui provides terminal-safe styling for croc's command-line output.
package termui

import (
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

const (
	Bold   = "\x1b[1m"
	Red    = "\x1b[31m"
	Green  = "\x1b[32m"
	Yellow = "\x1b[33m"
	Cyan   = "\x1b[36m"
	Reset  = "\x1b[0m"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Output returns a Windows-aware writer and whether styling should be used.
func Output(file *os.File) (io.Writer, bool) {
	if file == nil {
		return io.Discard, false
	}

	isTerminal := isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())
	colorEnabled := shouldUseColor(os.Getenv("NO_COLOR"), os.Getenv("TERM"), isTerminal)
	if !colorEnabled {
		return file, false
	}

	return colorable.NewColorable(file), true
}

func shouldUseColor(noColor, terminalName string, isTerminal bool) bool {
	return isTerminal && noColor == "" && !strings.EqualFold(strings.TrimSpace(terminalName), "dumb")
}

// Color applies a semantic terminal style when color is enabled.
func Color(text, style string, enabled bool) string {
	if !enabled {
		return text
	}
	return style + text + Reset
}

// Plain removes terminal styling from text. It is useful when measuring or
// comparing strings that may contain ANSI sequences.
func Plain(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}

// Emphasis highlights routine labels and choices without assigning a status
// color to them.
func Emphasis(text string, enabled bool) string {
	return Color(text, Bold, enabled)
}

// Filename highlights a path like Git highlights file metadata: bold, using
// the terminal's own foreground color.
func Filename(text string, enabled bool) string {
	return Emphasis(text, enabled)
}

// Secret highlights a share code like Git highlights an object identifier.
func Secret(text string, enabled bool) string {
	return Color(text, Yellow, enabled)
}

// Success highlights completed operations.
func Success(text string, enabled bool) string {
	return Color(text, Green, enabled)
}

// Warning highlights cautions and recoverable problems.
func Warning(text string, enabled bool) string {
	return Color(text, Yellow, enabled)
}

// Error highlights fatal errors.
func Error(text string, enabled bool) string {
	return Color(text, Red, enabled)
}

// PromptChoices highlights conventional yes/no choices without styling the
// filenames, paths, or other user-controlled data in the prompt.
func PromptChoices(prompt string, enabled bool) string {
	if !enabled {
		return prompt
	}
	for _, choice := range []string{"(Y/n)", "(y/N)"} {
		prompt = strings.ReplaceAll(prompt, choice, Emphasis(choice, true))
	}
	return prompt
}

// LoggerOutput normalizes the logger's Linux-only ANSI prefixes and applies
// croc's portable warning and error styles.
func LoggerOutput(file *os.File) io.Writer {
	output, enabled := Output(file)
	return &loggerWriter{output: output, colorEnabled: enabled}
}

type loggerWriter struct {
	output       io.Writer
	colorEnabled bool
}

func (w *loggerWriter) Write(p []byte) (int, error) {
	message := ansiPattern.ReplaceAllString(string(p), "")
	message = strings.Replace(message, "[warn]", Warning("[warn]", w.colorEnabled), 1)
	message = strings.Replace(message, "[error]", Error("[error]", w.colorEnabled), 1)
	if _, err := io.WriteString(w.output, message); err != nil {
		return 0, err
	}
	return len(p), nil
}
