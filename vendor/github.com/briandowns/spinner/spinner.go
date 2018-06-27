// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package spinner is a simple package to add a spinner / progress indicator to any terminal application.
package spinner

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
	"encoding/hex"

	"github.com/fatih/color"
)

// errInvalidColor is returned when attempting to set an invalid color
var errInvalidColor = errors.New("invalid color")

// validColors holds an array of the only colors allowed
var validColors = map[string]bool{
	// default colors for backwards compatibility
	"black":   true,
	"red":     true,
	"green":   true,
	"yellow":  true,
	"blue":    true,
	"magenta": true,
	"cyan":    true,
	"white":   true,

	// attributes
	"reset":        true,
	"bold":         true,
	"faint":        true,
	"italic":       true,
	"underline":    true,
	"blinkslow":    true,
	"blinkrapid":   true,
	"reversevideo": true,
	"concealed":    true,
	"crossedout":   true,

	// foreground text
	"fgBlack":   true,
	"fgRed":     true,
	"fgGreen":   true,
	"fgYellow":  true,
	"fgBlue":    true,
	"fgMagenta": true,
	"fgCyan":    true,
	"fgWhite":   true,

	// foreground Hi-Intensity text
	"fgHiBlack":   true,
	"fgHiRed":     true,
	"fgHiGreen":   true,
	"fgHiYellow":  true,
	"fgHiBlue":    true,
	"fgHiMagenta": true,
	"fgHiCyan":    true,
	"fgHiWhite":   true,

	// background text
	"bgBlack":   true,
	"bgRed":     true,
	"bgGreen":   true,
	"bgYellow":  true,
	"bgBlue":    true,
	"bgMagenta": true,
	"bgCyan":    true,
	"bgWhite":   true,

	// background Hi-Intensity text
	"bgHiBlack":   true,
	"bgHiRed":     true,
	"bgHiGreen":   true,
	"bgHiYellow":  true,
	"bgHiBlue":    true,
	"bgHiMagenta": true,
	"bgHiCyan":    true,
	"bgHiWhite":   true,
}

// returns a valid color's foreground text color attribute
var colorAttributeMap = map[string]color.Attribute{
	// default colors for backwards compatibility
	"black":   color.FgBlack,
	"red":     color.FgRed,
	"green":   color.FgGreen,
	"yellow":  color.FgYellow,
	"blue":    color.FgBlue,
	"magenta": color.FgMagenta,
	"cyan":    color.FgCyan,
	"white":   color.FgWhite,

	// attributes
	"reset":        color.Reset,
	"bold":         color.Bold,
	"faint":        color.Faint,
	"italic":       color.Italic,
	"underline":    color.Underline,
	"blinkslow":    color.BlinkSlow,
	"blinkrapid":   color.BlinkRapid,
	"reversevideo": color.ReverseVideo,
	"concealed":    color.Concealed,
	"crossedout":   color.CrossedOut,

	// foreground text colors
	"fgBlack":   color.FgBlack,
	"fgRed":     color.FgRed,
	"fgGreen":   color.FgGreen,
	"fgYellow":  color.FgYellow,
	"fgBlue":    color.FgBlue,
	"fgMagenta": color.FgMagenta,
	"fgCyan":    color.FgCyan,
	"fgWhite":   color.FgWhite,

	// foreground Hi-Intensity text colors
	"fgHiBlack":   color.FgHiBlack,
	"fgHiRed":     color.FgHiRed,
	"fgHiGreen":   color.FgHiGreen,
	"fgHiYellow":  color.FgHiYellow,
	"fgHiBlue":    color.FgHiBlue,
	"fgHiMagenta": color.FgHiMagenta,
	"fgHiCyan":    color.FgHiCyan,
	"fgHiWhite":   color.FgHiWhite,

	// background text colors
	"bgBlack":   color.BgBlack,
	"bgRed":     color.BgRed,
	"bgGreen":   color.BgGreen,
	"bgYellow":  color.BgYellow,
	"bgBlue":    color.BgBlue,
	"bgMagenta": color.BgMagenta,
	"bgCyan":    color.BgCyan,
	"bgWhite":   color.BgWhite,

	// background Hi-Intensity text colors
	"bgHiBlack":   color.BgHiBlack,
	"bgHiRed":     color.BgHiRed,
	"bgHiGreen":   color.BgHiGreen,
	"bgHiYellow":  color.BgHiYellow,
	"bgHiBlue":    color.BgHiBlue,
	"bgHiMagenta": color.BgHiMagenta,
	"bgHiCyan":    color.BgHiCyan,
	"bgHiWhite":   color.BgHiWhite,
}

// validColor will make sure the given color is actually allowed
func validColor(c string) bool {
	valid := false
	if validColors[c] {
		valid = true
	}
	return valid
}

// Spinner struct to hold the provided options
type Spinner struct {
	Delay      time.Duration                 // Delay is the speed of the indicator
	chars      []string                      // chars holds the chosen character set
	Prefix     string                        // Prefix is the text preppended to the indicator
	Suffix     string                        // Suffix is the text appended to the indicator
	FinalMSG   string                        // string displayed after Stop() is called
	lastOutput string                        // last character(set) written
	color      func(a ...interface{}) string // default color is white
	lock       *sync.RWMutex                 //
	Writer     io.Writer                     // to make testing better, exported so users have access
	active     bool                          // active holds the state of the spinner
	stopChan   chan struct{}                 // stopChan is a channel used to stop the indicator
}

// New provides a pointer to an instance of Spinner with the supplied options
func New(cs []string, d time.Duration) *Spinner {
	return &Spinner{
		Delay:    d,
		chars:    cs,
		color:    color.New(color.FgWhite).SprintFunc(),
		lock:     &sync.RWMutex{},
		Writer:   color.Output,
		active:   false,
		stopChan: make(chan struct{}, 1),
	}
}

// Active will return whether or not the spinner is currently active
func (s *Spinner) Active() bool {
	return s.active
}

// Start will start the indicator
func (s *Spinner) Start() {
	if s.active {
		return
	}
	s.active = true

	go func() {
		for {
			for i := 0; i < len(s.chars); i++ {
				select {
				case <-s.stopChan:
					return
				default:
					s.lock.Lock()
					s.erase()
					outColor := fmt.Sprintf("%s%s%s ", s.Prefix, s.color(s.chars[i]), s.Suffix)
					outPlain := fmt.Sprintf("%s%s%s ", s.Prefix, s.chars[i], s.Suffix)
					fmt.Fprint(s.Writer, outColor)
					s.lastOutput = outPlain
					delay := s.Delay
					s.lock.Unlock()

					time.Sleep(delay)
				}
			}
		}
	}()
}

// Stop stops the indicator
func (s *Spinner) Stop() {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.active {
		s.active = false
		s.erase()
		if s.FinalMSG != "" {
			fmt.Fprintf(s.Writer, s.FinalMSG)
		}
		s.stopChan <- struct{}{}
	}
}

// Restart will stop and start the indicator
func (s *Spinner) Restart() {
	s.Stop()
	s.Start()
}

// Reverse will reverse the order of the slice assigned to the indicator
func (s *Spinner) Reverse() {
	s.lock.Lock()
	defer s.lock.Unlock()
	for i, j := 0, len(s.chars)-1; i < j; i, j = i+1, j-1 {
		s.chars[i], s.chars[j] = s.chars[j], s.chars[i]
	}
}

// Color will set the struct field for the given color to be used
func (s *Spinner) Color(colors ...string) error {

	colorAttributes := make([]color.Attribute, len(colors))

	// Verify colours are valid and place the appropriate attribute in the array
	for index, c := range colors {
		if !validColor(c) {
			return errInvalidColor
		}

		colorAttributes[index] = colorAttributeMap[c]
	}

	s.color = color.New(colorAttributes...).SprintFunc()
	s.Restart()
	return nil
}

// UpdateSpeed will set the indicator delay to the given value
func (s *Spinner) UpdateSpeed(d time.Duration) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.Delay = d
}

// UpdateCharSet will change the current character set to the given one
func (s *Spinner) UpdateCharSet(cs []string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.chars = cs
}

// erase deletes written characters
//
// Caller must already hold s.lock.
func (s *Spinner) erase() {
	n := utf8.RuneCountInString(s.lastOutput)
	del, _ := hex.DecodeString("7f")
	for _, c := range []string{"\b", string(del), "\b"} {
		for i := 0; i < n; i++ {
			fmt.Fprintf(s.Writer, c)
		}
	}
	s.lastOutput = ""
}

// GenerateNumberSequence will generate a slice of integers at the
// provided length and convert them each to a string
func GenerateNumberSequence(length int) []string {
	numSeq := make([]string, length)
	for i := 0; i < length; i++ {
		numSeq[i] = strconv.Itoa(i)
	}
	return numSeq
}
