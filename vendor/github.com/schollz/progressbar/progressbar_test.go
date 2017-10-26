package progressbar

func ExampleBar() {
	bar := New(10)
	bar.Add(1)

	// Output:
	// 10% |████                                    | [0s:0s]
}
