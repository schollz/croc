package uiprogress

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gosuri/uiprogress/util/strutil"
)

var (
	// Fill is the default character representing completed progress
	Fill byte = '='

	// Head is the default character that moves when progress is updated
	Head byte = '>'

	// Empty is the default character that represents the empty progress
	Empty byte = '-'

	// LeftEnd is the default character in the left most part of the progress indicator
	LeftEnd byte = '['

	// RightEnd is the default character in the right most part of the progress indicator
	RightEnd byte = ']'

	// Width is the default width of the progress bar
	Width = 70

	// ErrMaxCurrentReached is error when trying to set current value that exceeds the total value
	ErrMaxCurrentReached = errors.New("errors: current value is greater total value")
)

// Bar represents a progress bar
type Bar struct {
	// Total of the total  for the progress bar
	Total int

	// LeftEnd is character in the left most part of the progress indicator. Defaults to '['
	LeftEnd byte

	// RightEnd is character in the right most part of the progress indicator. Defaults to ']'
	RightEnd byte

	// Fill is the character representing completed progress. Defaults to '='
	Fill byte

	// Head is the character that moves when progress is updated.  Defaults to '>'
	Head byte

	// Empty is the character that represents the empty progress. Default is '-'
	Empty byte

	// TimeStated is time progress began
	TimeStarted time.Time

	// Width is the width of the progress bar
	Width int

	// timeElased is the time elapsed for the progress
	timeElapsed time.Duration
	current     int

	mtx *sync.RWMutex

	appendFuncs  []DecoratorFunc
	prependFuncs []DecoratorFunc
}

// DecoratorFunc is a function that can be prepended and appended to the progress bar
type DecoratorFunc func(b *Bar) string

// NewBar returns a new progress bar
func NewBar(total int) *Bar {
	return &Bar{
		Total:    total,
		Width:    Width,
		LeftEnd:  LeftEnd,
		RightEnd: RightEnd,
		Head:     Head,
		Fill:     Fill,
		Empty:    Empty,

		mtx: &sync.RWMutex{},
	}
}

// Set the current count of the bar. It returns ErrMaxCurrentReached when trying n exceeds the total value. This is atomic operation and concurancy safe.
func (b *Bar) Set(n int) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if n > b.Total {
		return ErrMaxCurrentReached
	}
	b.current = n
	return nil
}

// Incr increments the current value by 1, time elapsed to current time and returns true. It returns false if the cursor has reached or exceeds total value.
func (b *Bar) Incr() bool {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	n := b.current + 1
	if n > b.Total {
		return false
	}
	var t time.Time
	if b.TimeStarted == t {
		b.TimeStarted = time.Now()
	}
	b.timeElapsed = time.Since(b.TimeStarted)
	b.current = n
	return true
}

// Current returns the current progress of the bar
func (b *Bar) Current() int {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.current
}

// AppendFunc runs the decorator function and renders the output on the right of the progress bar
func (b *Bar) AppendFunc(f DecoratorFunc) *Bar {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.appendFuncs = append(b.appendFuncs, f)
	return b
}

// AppendCompleted appends the completion percent to the progress bar
func (b *Bar) AppendCompleted() *Bar {
	b.AppendFunc(func(b *Bar) string {
		return b.CompletedPercentString()
	})
	return b
}

// AppendElapsed appends the time elapsed the be progress bar
func (b *Bar) AppendElapsed() *Bar {
	b.AppendFunc(func(b *Bar) string {
		return strutil.PadLeft(b.TimeElapsedString(), 5, ' ')
	})
	return b
}

// PrependFunc runs decorator function and render the output left the progress bar
func (b *Bar) PrependFunc(f DecoratorFunc) *Bar {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.prependFuncs = append(b.prependFuncs, f)
	return b
}

// PrependCompleted prepends the precent completed to the progress bar
func (b *Bar) PrependCompleted() *Bar {
	b.PrependFunc(func(b *Bar) string {
		return b.CompletedPercentString()
	})
	return b
}

// PrependElapsed prepends the time elapsed to the begining of the bar
func (b *Bar) PrependElapsed() *Bar {
	b.PrependFunc(func(b *Bar) string {
		return strutil.PadLeft(b.TimeElapsedString(), 5, ' ')
	})
	return b
}

// Bytes returns the byte presentation of the progress bar
func (b *Bar) Bytes() []byte {
	completedWidth := int(float64(b.Width) * (b.CompletedPercent() / 100.00))

	// add fill and empty bits
	var buf bytes.Buffer
	for i := 0; i < completedWidth; i++ {
		buf.WriteByte(b.Fill)
	}
	for i := 0; i < b.Width-completedWidth; i++ {
		buf.WriteByte(b.Empty)
	}

	// set head bit
	pb := buf.Bytes()
	if completedWidth > 0 && completedWidth < b.Width {
		pb[completedWidth-1] = b.Head
	}

	// set left and right ends bits
	pb[0], pb[len(pb)-1] = b.LeftEnd, b.RightEnd

	// render append functions to the right of the bar
	for _, f := range b.appendFuncs {
		pb = append(pb, ' ')
		pb = append(pb, []byte(f(b))...)
	}

	// render prepend functions to the left of the bar
	for _, f := range b.prependFuncs {
		args := []byte(f(b))
		args = append(args, ' ')
		pb = append(args, pb...)
	}
	return pb
}

// String returns the string representation of the bar
func (b *Bar) String() string {
	return string(b.Bytes())
}

// CompletedPercent return the percent completed
func (b *Bar) CompletedPercent() float64 {
	return (float64(b.Current()) / float64(b.Total)) * 100.00
}

// CompletedPercentString returns the formatted string representation of the completed percent
func (b *Bar) CompletedPercentString() string {
	return fmt.Sprintf("%3.f%%", b.CompletedPercent())
}

// TimeElapsed returns the time elapsed
func (b *Bar) TimeElapsed() time.Duration {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.timeElapsed
}

// TimeElapsedString returns the formatted string represenation of the time elapsed
func (b *Bar) TimeElapsedString() string {
	return strutil.PrettyTime(b.TimeElapsed())
}
