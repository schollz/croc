package progressbar

import "time"

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
