# uiprogress [![GoDoc](https://godoc.org/github.com/gosuri/uiprogress?status.svg)](https://godoc.org/github.com/gosuri/uiprogress) [![Build Status](https://travis-ci.org/gosuri/uiprogress.svg?branch=master)](https://travis-ci.org/gosuri/uiprogress)

A Go library to render progress bars in terminal applications. It provides a set of flexible features with a customizable API.

![example](doc/example_full.gif)

Progress bars improve readability for terminal applications with long outputs by providing a concise feedback loop.

## Features

* __Multiple Bars__: uiprogress can render multiple progress bars that can be tracked concurrently
* __Dynamic Addition__:  Add additional progress bars any time, even after the progress tracking has started
* __Prepend and Append Functions__: Append or prepend completion percent and time elapsed to the progress bars
* __Custom Decorator Functions__: Add custom functions around the bar along with helper functions

## Usage

To start listening for progress bars, call `uiprogress.Start()` and add a progress bar using `uiprogress.AddBar(total int)`. Update the progress using `bar.Incr()` or `bar.Set(n int)`. Full source code for the below example is available at [example/simple/simple.go](example/simple/simple.go) 

```go
uiprogress.Start()            // start rendering
bar := uiprogress.AddBar(100) // Add a new bar

// optionally, append and prepend completion and elapsed time
bar.AppendCompleted()
bar.PrependElapsed()

for bar.Incr() {
  time.Sleep(time.Millisecond * 20)
}
```

This will render the below in the terminal

![example](doc/example_simple.gif)

### Using Custom Decorators

You can also add a custom decorator function in addition to default `bar.AppendCompleted()` and `bar.PrependElapsed()` decorators. The below example tracks the current step for an application deploy progress. Source code for the below example is available at [example/full/full.go](example/full/full.go) 

```go
var steps = []string{"downloading source", "installing deps", "compiling", "packaging", "seeding database", "deploying", "staring servers"}
bar := uiprogress.AddBar(len(steps))

// prepend the current step to the bar
bar.PrependFunc(func(b *uiprogress.Bar) string {
  return "app: " + steps[b.Current()-1]
})

for bar.Incr() {
  time.Sleep(time.Millisecond * 10)
}
```

### Rendering Multiple bars

You can add multiple bars using `uiprogress.AddBar(n)`. The below example demonstrates updating multiple bars concurrently and adding a new bar later in the pipeline. Source for this example is available at [example/multi/multi.go](example/multi/multi.go) 

```go
waitTime := time.Millisecond * 100
uiprogress.Start()

// start the progress bars in go routines
var wg sync.WaitGroup

bar1 := uiprogress.AddBar(20).AppendCompleted().PrependElapsed()
wg.Add(1)
go func() {
  defer wg.Done()
  for bar1.Incr() {
    time.Sleep(waitTime)
  }
}()

bar2 := uiprogress.AddBar(40).AppendCompleted().PrependElapsed()
wg.Add(1)
go func() {
  defer wg.Done()
  for bar2.Incr() {
    time.Sleep(waitTime)
  }
}()

time.Sleep(time.Second)
bar3 := uiprogress.AddBar(20).PrependElapsed().AppendCompleted()
wg.Add(1)
go func() {
  defer wg.Done()
  for i := 1; i <= bar3.Total; i++ {
    bar3.Set(i)
    time.Sleep(waitTime)
  }
}()

// wait for all the go routines to finish
wg.Wait()
```

This will produce

![example](doc/example_multi.gif)

### `Incr` counter

[Bar.Incr()](https://godoc.org/github.com/gosuri/uiprogress#Bar.Incr) is an atomic counter and can be used as a general tracker, making it ideal for tracking progress of work fanned out to a lots of go routines. The source code for the below example is available at [example/incr/incr.go](example/incr/incr.go)

```go
runtime.GOMAXPROCS(runtime.NumCPU()) // use all available cpu cores

// create a new bar and prepend the task progress to the bar and fanout into 1k go routines
count := 1000
bar := uiprogress.AddBar(count).AppendCompleted().PrependElapsed()
bar.PrependFunc(func(b *uiprogress.Bar) string {
  return fmt.Sprintf("Task (%d/%d)", b.Current(), count)
})

uiprogress.Start()
var wg sync.WaitGroup

// fanout into go routines
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
```

## Installation

```sh
$ go get -v github.com/gosuri/uiprogress
```
## Todos

- [ ] Resize bars and decorators by auto detecting window's dimensions
- [ ] Handle more progress bars than vertical screen allows

## License

uiprogress is released under the MIT License. See [LICENSE](https://github.com/gosuri/uiprogress/blob/master/LICENSE).
