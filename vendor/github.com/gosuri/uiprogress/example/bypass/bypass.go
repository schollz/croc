package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/gosuri/uiprogress"
)

func main() {
	waitTime := time.Millisecond * 200
	p := uiprogress.New()
	p.Start()

	var wg sync.WaitGroup

	bar1 := p.AddBar(20).AppendCompleted().PrependElapsed()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for bar1.Incr() {
			time.Sleep(waitTime)
		}
		fmt.Fprintln(p.Bypass(), "Bar1 finished")
	}()

	bar2 := p.AddBar(40).AppendCompleted().PrependElapsed()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for bar2.Incr() {
			time.Sleep(waitTime)
		}
		fmt.Fprintln(p.Bypass(), "Bar2 finished")
	}()

	time.Sleep(time.Second)
	bar3 := p.AddBar(20).PrependElapsed().AppendCompleted()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for bar3.Incr() {
			time.Sleep(waitTime)
		}
		fmt.Fprintln(p.Bypass(), "Bar3 finished")
	}()

	wg.Wait()
}
