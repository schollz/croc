# progressbar

[![travis](https://travis-ci.org/schollz/progressbar.svg?branch=master)](https://travis-ci.org/schollz/progressbar) 
[![go report card](https://goreportcard.com/badge/github.com/schollz/progressbar)](https://goreportcard.com/report/github.com/schollz/progressbar) 
[![coverage](https://img.shields.io/badge/coverage-94%25-brightgreen.svg)](https://gocover.io/github.com/schollz/progressbar)
[![godocs](https://godoc.org/github.com/schollz/progressbar?status.svg)](https://godoc.org/github.com/schollz/progressbar) 

A very simple thread-safe progress bar which should work on every OS without problems. I needed a progressbar for [croc](https://github.com/schollz/croc) and everything I tried had problems, so I made another one.

![Example of progress bar](https://user-images.githubusercontent.com/6550035/32120326-5f420d42-bb15-11e7-89d4-c502864e78eb.gif)

## Install

```
go get -u github.com/schollz/progressbar
```

## Usage 

**Basic usage:**

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

## Contributing

Pull requests are welcome. Feel free to...

- Revise documentation
- Add new features
- Fix bugs
- Suggest improvements

## License

MIT
