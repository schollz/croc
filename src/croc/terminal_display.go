package croc

import (
	"bytes"
	"io"
	"os"
	"strings"
	"time"

	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/progressbar/v3"
)

func (c *Client) newProgressBar(max int64, description string, throttle time.Duration) *progressbar.ProgressBar {
	output, colorEnabled := termui.Output(os.Stderr)
	description = styleProgressFilename(description, colorEnabled)
	options := []progressbar.Option{
		progressbar.OptionOnCompletion(func() {
			c.fmtPrintUpdate()
		}),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWriter(progressBarWriter(output, colorEnabled)),
		progressbar.OptionSetVisibility(!c.Options.SendingText),
		progressbar.OptionEnableColorCodes(colorEnabled),
		progressbar.OptionSetTheme(progressBarTheme(colorEnabled)),
	}
	if throttle > 0 {
		options = append(options, progressbar.OptionThrottle(throttle))
	}
	return progressbar.NewOptions64(max, options...)
}

func styleProgressFilename(description string, colorEnabled bool) string {
	if !colorEnabled {
		return description
	}
	filename := strings.Trim(description, " ")
	if filename == "" {
		return description
	}
	leading := description[:len(description)-len(strings.TrimLeft(description, " "))]
	trailing := description[len(strings.TrimRight(description, " ")):]
	return leading + termui.Filename(filename, true) + trailing
}

func quotedFilename(name string, colorEnabled bool) string {
	return "'" + termui.Filename(name, colorEnabled) + "'"
}

func progressBarTheme(colorEnabled bool) progressbar.Theme {
	if !colorEnabled {
		return progressbar.ThemeDefault
	}
	return progressbar.Theme{
		Saucer:         "█",
		SaucerHead:     "█[reset]",
		SaucerPadding:  " ",
		BarStart:       "|",
		BarStartFilled: "|[cyan]",
		BarEnd:         "|",
		BarEndFilled:   "[reset]|",
	}
}

func progressBarWriter(output io.Writer, colorEnabled bool) io.Writer {
	if !colorEnabled {
		return output
	}
	return &completionColorWriter{output: output}
}

// completionColorWriter keeps a transfer cyan while it is active, then changes
// the final 100% render to green. It sits before the Windows ANSI
// translator so the same replacement works on every supported platform.
type completionColorWriter struct {
	output io.Writer
}

func (w *completionColorWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("100% |"+termui.Cyan)) {
		p = bytes.ReplaceAll(p, []byte(termui.Cyan), []byte(termui.Green))
	}
	n, err := w.output.Write(p)
	if n == len(p) {
		return len(p), err
	}
	return n, err
}
