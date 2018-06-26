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

	"github.com/fatih/color"
)

// errInvalidColor is returned when attempting to set an invalid color
var errInvalidColor = errors.New("invalid color")

// validColors holds an array of the only colors allowed
var validColors = map[string]bool{
	"red":     true,
	"green":   true,
	"yellow":  true,
	"blue":    true,
	"magenta": true,
	"cyan":    true,
	"white":   true,
}

// returns a valid color's foreground text color attribute
var colorAttributeMap = map[string]color.Attribute{
	"red":     color.FgRed,
	"green":   color.FgGreen,
	"yellow":  color.FgYellow,
	"blue":    color.FgBlue,
	"magenta": color.FgMagenta,
	"cyan":    color.FgCyan,
	"white":   color.FgWhite,
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
func (s *Spinner) Color(c string) error {
	if !validColor(c) {
		return errInvalidColor
	}
	s.color = color.New(colorAttributeMap[c]).SprintFunc()
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
	for _, c := range []string{"\b", " ", "\b"} {
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
