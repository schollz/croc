<p align="center">
<img
    src="logo.png"
    width="100%" border="0" alt="progressbar">
<br>
<a href="https://travis-ci.org/schollz/progressbar"><img src="https://travis-ci.org/schollz/progressbar.svg?branch=master" alt="Build Status"></a>
<img src="https://img.shields.io/badge/coverage-94%25-brightgreen.svg" alt="Code Coverage">
<a href="https://goreportcard.com/report/github.com/schollz/progressbar"><img src="https://goreportcard.com/badge/github.com/schollz/progressbar" alt="Go Report Card"></a>
<a href="https://godoc.org/github.com/schollz/progressbar"><img src="https://godoc.org/github.com/schollz/progressbar?status.svg" alt="GoDoc"></a>
</p>

<p align="center">A very simple progress bar.</p>

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
