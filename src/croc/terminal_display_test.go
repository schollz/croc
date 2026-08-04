package croc

import (
	"strings"
	"testing"

	"github.com/schollz/progressbar/v3"
)

func TestProgressBarTheme(t *testing.T) {
	plain := progressBarTheme(false)
	if plain != progressbar.ThemeDefault {
		t.Fatalf("plain progress theme = %#v; want default theme", plain)
	}

	colored := progressBarTheme(true)
	if colored.BarStartFilled != "|[cyan]" {
		t.Fatalf("colored bar start = %q; want cyan", colored.BarStartFilled)
	}
	if colored.SaucerHead != "█[reset]" || colored.BarEndFilled != "[reset]|" {
		t.Fatalf("colored progress theme does not reset after its filled section: %#v", colored)
	}
}

func TestStyleProgressFilename(t *testing.T) {
	const description = "  croc.txt  "
	if got := styleProgressFilename(description, false); got != description {
		t.Fatalf("plain description = %q; want %q", got, description)
	}
	want := "  \x1b[1mcroc.txt\x1b[0m  "
	if got := styleProgressFilename(description, true); got != want {
		t.Fatalf("styled description = %q; want %q", got, want)
	}
	if got := styleProgressFilename("   ", true); got != "   " {
		t.Fatalf("blank description = %q; want spaces unchanged", got)
	}
}

func TestQuotedFilename(t *testing.T) {
	if got := quotedFilename("croc.txt", false); got != "'croc.txt'" {
		t.Fatalf("plain filename = %q", got)
	}
	if got := quotedFilename("croc.txt", true); got != "'\x1b[1mcroc.txt\x1b[0m'" {
		t.Fatalf("styled filename = %q", got)
	}
}

func TestColoredProgressBarThemeRendersANSIWithoutMarkup(t *testing.T) {
	var output strings.Builder
	bar := progressbar.NewOptions64(2,
		progressbar.OptionSetWriter(&output),
		progressbar.OptionSetWidth(2),
		progressbar.OptionSetTheme(progressBarTheme(true)),
		progressbar.OptionEnableColorCodes(true),
	)
	if err := bar.Add(1); err != nil {
		t.Fatalf("render progress bar: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("colored progress bar has no ANSI styling: %q", got)
	}
	if strings.Contains(got, "[green]") || strings.Contains(got, "[reset]") {
		t.Fatalf("progress bar leaked color markup: %q", got)
	}
	if !strings.Contains(got, "\x1b[36m") {
		t.Fatalf("active progress bar is not cyan: %q", got)
	}
}

func TestProgressBarWriterUsesGreenOnlyAtCompletion(t *testing.T) {
	var output strings.Builder
	writer := progressBarWriter(&output, true)

	active := "100% legit  50% |\x1b[36m█\x1b[0m |"
	if _, err := writer.Write([]byte(active)); err != nil {
		t.Fatalf("write active progress: %v", err)
	}
	if got := output.String(); got != active {
		t.Fatalf("active progress = %q; want %q", got, active)
	}

	output.Reset()
	complete := "100% |\x1b[36m██\x1b[0m|"
	if _, err := writer.Write([]byte(complete)); err != nil {
		t.Fatalf("write completed progress: %v", err)
	}
	want := "100% |\x1b[32m██\x1b[0m|"
	if got := output.String(); got != want {
		t.Fatalf("completed progress = %q; want %q", got, want)
	}

	plain := progressBarWriter(&output, false)
	if plain != &output {
		t.Fatalf("plain progress writer = %T; want original writer", plain)
	}
}
