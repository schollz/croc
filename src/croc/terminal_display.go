package croc

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/progressbar/v3"
)

const (
	receiveStatusLookingForSender        = "looking for sender..."
	receiveStatusConnecting              = "connecting..."
	receiveStatusWaitingForSender        = "waiting for sender..."
	receiveStatusAuthenticatingCode      = "authenticating code..."
	receiveStatusOpeningTransferChannels = "opening transfer channels..."
	receiveStatusWaitingForFileList      = "waiting for file list..."
)

func writeReceiveStatus(output io.Writer, previousWidth int, status string) int {
	if previousWidth > 0 {
		_, _ = io.WriteString(output, "\r")
	}
	_, _ = io.WriteString(output, status)
	if padding := previousWidth - len(status); padding > 0 {
		_, _ = io.WriteString(output, strings.Repeat(" ", padding))
	}
	return len(status)
}

func clearReceiveStatus(output io.Writer, width int) {
	if width == 0 {
		return
	}
	_, _ = io.WriteString(output, "\r"+strings.Repeat(" ", width)+"\r")
}

func (c *Client) setReceiveStatus(status string) {
	output, _ := termui.Output(os.Stderr)
	c.receiveStatusWidth = writeReceiveStatus(output, c.receiveStatusWidth, status)
}

func (c *Client) clearReceiveStatus() {
	if c.receiveStatusWidth == 0 {
		return
	}
	output, _ := termui.Output(os.Stderr)
	clearReceiveStatus(output, c.receiveStatusWidth)
	c.receiveStatusWidth = 0
}

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

func peerIP(address string) string {
	address = strings.TrimSpace(address)
	if host, _, err := net.SplitHostPort(address); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(address, "[]")
}

func formatTransferDirection(isSender, peerToPeer bool, localAddress, peerAddress string) string {
	local := peerIP(localAddress)
	peer := peerIP(peerAddress)
	if !peerToPeer || local == "" {
		if isSender {
			return "->" + peer
		}
		return "<-" + peer
	}

	if isSender {
		return local + "->" + peer
	}
	return local + "<-" + peer
}

func (c *Client) peerToPeerDataPath() bool {
	switch c.selectedDataTransport.Load() {
	case selectedTransportRelay:
		address, _ := c.currentRelayControlRoute()
		return c.relayControlRouteIsPeerToPeer(address)
	case selectedTransportTailcat:
		c.tailcat.bundleMu.Lock()
		defer c.tailcat.bundleMu.Unlock()
		return tailcatBundlePath(c.tailcat.bundle) == "direct"
	default:
		return false
	}
}

func (c *Client) transferDirection() string {
	return formatTransferDirection(
		c.Options.IsSender,
		c.peerToPeerDataPath(),
		c.ExternalIP,
		c.ExternalIPConnected,
	)
}

func preferredPeerIP(fallback, relayObserved string) string {
	fallback = peerIP(fallback)
	observed := peerIP(relayObserved)
	if isPublicIP(observed) {
		return observed
	}
	if isPublicIP(fallback) {
		return fallback
	}
	if observed != "" {
		return observed
	}
	return fallback
}

func preferredPublicIP(relayObserved string, localAddresses []string) string {
	observed := peerIP(relayObserved)
	if isPublicIP(observed) {
		return observed
	}
	for _, address := range localAddresses {
		if candidate := peerIP(address); isPublicIP(candidate) {
			return candidate
		}
	}
	return observed
}

func isPublicIP(address string) bool {
	ip := net.ParseIP(address)
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate()
}

func formatNoTransferSummary(files []FileInfo, unchanged int, colorEnabled bool) string {
	if unchanged > 0 && unchanged == len(files) {
		detail := fmt.Sprintf("all %d files", unchanged)
		if unchanged == 1 {
			detail = quotedFilename(path.Join(files[0].FolderRemote, files[0].Name), colorEnabled)
		}
		return "\rAlready up to date: " + detail + "\n"
	}
	return "\rNo files transferred.\n"
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
