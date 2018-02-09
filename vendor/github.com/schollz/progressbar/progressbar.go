package progressbar

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
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

	theme []string

	sync.RWMutex
}

func (p *ProgressBar) SetTheme(theme []string) {
	p.Lock()
	p.theme = theme
	p.Unlock()
}

// New returns a new ProgressBar
// with the specified maximum
func New(max int) *ProgressBar {
	return &ProgressBar{
		max:       max,
		size:      40,
		theme:     []string{"█", " ", "|", "|"},
		w:         os.Stdout,
		lastShown: time.Now(),
		startTime: time.Now(),
	}
}

// Reset will reset the clock that is used
// to calculate current time and the time left.
func (p *ProgressBar) Reset() {
	p.Lock()
	defer p.Unlock()
	p.lastShown = time.Now()
	p.startTime = time.Now()
	p.currentNum = 0
}

// SetMax sets the total number of the progress bar
func (p *ProgressBar) SetMax(num int) {
	p.Lock()
	defer p.Unlock()
	p.max = num
}

// SetSize sets the size of the progress bar.
func (p *ProgressBar) SetSize(size int) {
	p.Lock()
	defer p.Unlock()
	p.size = size
}

// Add with increase the current count on the progress bar
func (p *ProgressBar) Add(num int) error {
	p.RLock()
	currentNum := p.currentNum
	p.RUnlock()
	return p.Set(currentNum + num)
}

// Set will change the current count on the progress bar
func (p *ProgressBar) Set(num int) error {
	p.Lock()
	if p.max == 0 {
		p.Unlock()
		return errors.New("max must be greater than 0")
	}
	p.currentNum = num
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

	s := p.String()

	_, err := io.WriteString(p.w, s)
	if err != nil {
		return err
	}

	// handle mac os newline
	// this breaks your test for some reason
	if runtime.GOOS == "darwin" {
		fmt.Fprintf(p.w, "\033[%dA", 0)
	}

	if f, ok := p.w.(*os.File); ok {
		f.Sync()
	}
	return nil
}

func (p *ProgressBar) String() string {
	p.RLock()
	defer p.RUnlock()
	leftTime := time.Since(p.startTime).Seconds() / float64(p.currentNum) * (float64(p.max) - float64(p.currentNum))
	return fmt.Sprintf("\r%4d%% %s%s%s%s [%s:%s]            ",
		p.currentPercent,
		p.theme[2],
		strings.Repeat(p.theme[0], p.currentSaucerSize),
		strings.Repeat(p.theme[1], p.size-p.currentSaucerSize),
		p.theme[3],
		(time.Duration(time.Since(p.startTime).Seconds()) * time.Second).String(),
		(time.Duration(leftTime) * time.Second).String(),
	)
}
