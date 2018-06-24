# progressbar

[![travis](https://travis-ci.org/schollz/progressbar.svg?branch=master)](https://travis-ci.org/schollz/progressbar) 
[![go report card](https://goreportcard.com/badge/github.com/schollz/progressbar)](https://goreportcard.com/report/github.com/schollz/progressbar) 
[![coverage](https://img.shields.io/badge/coverage-92%25-brightgreen.svg)](https://gocover.io/github.com/schollz/progressbar)
[![godocs](https://godoc.org/github.com/schollz/progressbar?status.svg)](https://godoc.org/github.com/schollz/progressbar) 

A very simple thread-safe progress bar which should work on every OS without problems. I needed a progressbar for [croc](https://github.com/schollz/croc) and everything I tried had problems, so I made another one.

![Example of progress bar](https://user-images.githubusercontent.com/6550035/32120326-5f420d42-bb15-11e7-89d4-c502864e78eb.gif)

## Install

```
go get -u github.com/schollz/progressbar
```

## Usage 

### Basic usage

```golang
bar := progressbar.New(100)
for i := 0; i < 100; i++ {
    bar.Add(1)
    time.Sleep(10 * time.Millisecond)
}
```

which looks like:

```bash
 100% |████████████████████████████████████████| [1s:0s]
 ```

The times at the end show the elapsed time and the remaining time, respectively.

### Long running processes
For long running processes, you might want to render from a 0% state.

```golang
// Renders the bar right on construction
bar := progress.NewOptions(100, OptionSetRenderBlankState(true))
```

Alternatively, when you want to delay rendering, but still want to render a 0% state
```golang
bar := progress.NewOptions(100)

// Render the current state, which is 0% in this case
bar.RenderBlank()

// Emulate work
for i := 0; i < 10; i++ {
    time.Sleep(10 * time.Minute)
    bar.Add(10)
}
```

### Use a custom writer
The default writer is standard output (os.Stdout), but you can set it to whatever satisfies io.Writer.
```golang
bar := NewOptions(
    10,
    OptionSetTheme(Theme{Saucer: "#", SaucerPadding: "-", BarStart: ">", BarEnd: "<"}),
    OptionSetWidth(10),
    OptionSetWriter(&buf),
)

bar.Add(5)
result := strings.TrimSpace(buf.String())

// Result equals:
// 50% >#####-----< [0s:0s]

```


## Contributing

Pull requests are welcome. Feel free to...

- Revise documentation
- Add new features
- Fix bugs
- Suggest improvements

## Thanks

Thanks [@Dynom](https://github.com/dynom) for massive improvements in version 2.0!

Thanks [@CrushedPixel](https://github.com/CrushedPixel) for adding descriptions and color code support!

## License

MIT
