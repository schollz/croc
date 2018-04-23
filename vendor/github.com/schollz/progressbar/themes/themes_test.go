package themes

import "testing"

func TestNewDefault(t *testing.T) {
	if _, err := NewDefault(uint8(len(defaultSymbolsFinished) + 1)); err == nil {
		t.Error("should have an error if n > default themes")
	}
}
