package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"time"

	"github.com/gosuri/uiprogress"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU()) // use all available cpu cores

	// create a new bar and prepend the task progress to the bar and fanout into 1k go routines
	count := 1000
	bar := uiprogress.AddBar(count).AppendCompleted().PrependElapsed()
	bar.PrependFunc(func(b *uiprogress.Bar) string {
		return fmt.Sprintf("Task (%d/%d)", b.Current(), count)
	})

	uiprogress.Start()
	var wg sync.WaitGroup

	// fanout into 1k go routines
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(500)))
			bar.Incr()
		}()
	}
	time.Sleep(time.Second) // wait for a second for all the go routines to finish
	wg.Wait()
	uiprogress.Stop()
}
