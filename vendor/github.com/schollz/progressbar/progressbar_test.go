package progressbar

import (
	"testing"
	"time"
)

func ExampleBar() {
	bar := New(10)
	bar.SetMax(100)
	bar.SetSize(10)
	bar.Reset()
	time.Sleep(1 * time.Second)
	bar.Add(10)
	// Output:
	// 10% |█         | [1s:9s]
}

func TestBar(t *testing.T) {
	bar := New(0)
	if err := bar.Add(1); err == nil {
		t.Error("should have an error for 0 bar")
	}
	bar = New(10)
	if err := bar.Add(11); err == nil {
		t.Error("should have an error for adding > bar")
	}
}
