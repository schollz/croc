package progressbar

import (
	"io/ioutil"
	"strings"
	"testing"
	"time"
)

func ExampleProgressBar() {
	bar := NewOptions(100, OptionSetWidth(10), OptionSetRenderBlankState(false))
	bar.Reset()
	time.Sleep(1 * time.Second)
	bar.Add(10)
	// Output:
	// 10% |█         | [1s:9s]
}

func ExampleFinish() {
	bar := NewOptions(100, OptionSetWidth(10), OptionSetRenderBlankState(false))
	bar.Reset()
	bar.Finish()
	// Output:
	// 100% |██████████| [0s:0s]
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

func ExampleProgressBar_RenderBlank() {
	NewOptions(10, OptionSetWidth(10), OptionSetRenderBlankState(true))
	// Output:
	// 0% |          | [0s:0s]
}

func TestBasicSets(t *testing.T) {
	b := NewOptions(
		999,
		OptionSetWidth(888),
		OptionSetRenderBlankState(true),

		OptionSetWriter(ioutil.Discard), // suppressing output for this test
	)

	tc := b.config

	if tc.max != 999 {
		t.Errorf("Expected %s to be %d, instead I got %d\n%+v", "max", 999, tc.max, b)
	}

	if tc.width != 888 {
		t.Errorf("Expected %s to be %d, instead I got %d\n%+v", "width", 999, tc.max, b)
	}

	if !tc.renderWithBlankState {
		t.Errorf("Expected %s to be %t, instead I got %t\n%+v", "renderWithBlankState", true, tc.renderWithBlankState, b)
	}
}

func TestOptionSetTheme(t *testing.T) {
	buf := strings.Builder{}
	bar := NewOptions(
		10,
		OptionSetTheme(Theme{Saucer: "#", SaucerPadding: "-", BarStart: ">", BarEnd: "<"}),
		OptionSetWidth(10),
		OptionSetWriter(&buf),
	)

	bar.Add(5)
	result := strings.TrimSpace(buf.String())
	expect := "50% >#####-----< [0s:0s]"
	if result != expect {
		t.Errorf("Render miss-match\nResult: '%s'\nExpect: '%s'\n%+v", result, expect, bar)
	}
}
