package uiprogress

import (
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBarPrepend(t *testing.T) {
	b := NewBar(100)
	b.PrependCompleted()
	b.Set(50)
	if !strings.Contains(b.String(), "50") {
		t.Fatal("want", "50%", "in", b.String())
	}
}

func TestBarIncr(t *testing.T) {
	b := NewBar(10000)
	runtime.GOMAXPROCS(runtime.NumCPU())
	var wg sync.WaitGroup
	for i := 0; i < 10000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Incr()
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(10)))
		}()
	}
	wg.Wait()
	if b.Current() != 10000 {
		t.Fatal("need", 10000, "got", b.Current())
	}
}
