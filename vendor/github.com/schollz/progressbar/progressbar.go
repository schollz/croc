package progressbar

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// ProgressBar is a thread-safe, simple
// progress bar
type ProgressBar struct {
	max               int // max number of the counter
	size              int // size of the saucer
	currentNum        int
	currentPercent    int
	lastPercent       int
	currentSaucerSize int

	lastShown time.Time
	startTime time.Time
	w         io.Writer

	// symbols
	symbolFinished string
	symbolLeft     string
	leftBookend    string
	rightBookend   string
	sync.RWMutex
}

// New returns a new ProgressBar
// with the specified maximum
func New(max int) *ProgressBar {
	p := new(ProgressBar)
	p.Lock()
	defer p.Unlock()
	p.max = max
	p.size = 40
	p.symbolFinished = "█"
	p.symbolLeft = " "
	p.leftBookend = "|"
	p.rightBookend = "|"
	p.w = os.Stdout
	p.lastShown = time.Now()
	p.startTime = time.Now()
	return p
}

// Reset will reset the clock
func (p *ProgressBar) Reset() {
	p.Lock()
	defer p.Unlock()
	p.lastShown = time.Now()
	p.startTime = time.Now()
}

// Set the max of the progress bar
func (p *ProgressBar) SetMax(num int) {
	p.Lock()
	defer p.Unlock()
	p.max = num
}

// Add a certain amount to the progress bar
func (p *ProgressBar) Add(num int) error {
	p.Lock()
	p.currentNum += num
	percent := float64(p.currentNum) / float64(p.max)
	p.currentSaucerSize = int(percent * float64(p.size))
	p.currentPercent = int(percent * 100)
	updateBar := p.currentPercent != p.lastPercent && p.currentPercent > 0
	p.lastPercent = p.currentPercent
	p.Unlock()
	if updateBar {
		return p.Show()
	}
	return nil
}

// Show will print the current progress bar
func (p *ProgressBar) Show() error {
	p.RLock()
	defer p.RUnlock()
	if p.currentNum > p.max {
		return errors.New("current number exceeds max")
	}
	secondsLeft := time.Since(p.startTime).Seconds() / float64(p.currentNum) * (float64(p.max) - float64(p.currentNum))
	s := fmt.Sprintf("\r%3d%% %s%s%s%s [%s:%s]            ",
		p.currentPercent,
		p.leftBookend,
		strings.Repeat(p.symbolFinished, p.currentSaucerSize),
		strings.Repeat(p.symbolLeft, p.size-p.currentSaucerSize),
		p.rightBookend,
		time.Since(p.startTime).Round(time.Second).String(),
		(time.Duration(secondsLeft) * time.Second).String(),
	)

	_, err := io.WriteString(p.w, s)
	if err != nil {
		return err
	}
	if f, ok := p.w.(*os.File); ok {
		f.Sync()
	}
	return nil
}
