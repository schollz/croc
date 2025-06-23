package mnemonicode

import (
	"testing"
)

func TestWordsRequired(t *testing.T) {
	tests := []struct {
		name   string
		length int
		want   int
	}{
		{"empty", 0, 0},
		{"1 byte", 1, 1},
		{"2 bytes", 2, 2},
		{"3 bytes", 3, 3},
		{"4 bytes", 4, 3},
		{"5 bytes", 5, 4},
		{"8 bytes", 8, 6},
		{"12 bytes", 12, 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WordsRequired(tt.length); got != tt.want {
				t.Errorf("WordsRequired(%d) = %d, want %d", tt.length, got, tt.want)
			}
		})
	}
}

func TestEncodeWordList(t *testing.T) {
	tests := []struct {
		name string
		dst  []string
		src  []byte
		want int
	}{
		{
			name: "empty input",
			dst:  []string{},
			src:  []byte{},
			want: 0,
		},
		{
			name: "single byte",
			dst:  []string{},
			src:  []byte{0x01},
			want: 1,
		},
		{
			name: "two bytes",
			dst:  []string{},
			src:  []byte{0x01, 0x02},
			want: 2,
		},
		{
			name: "three bytes",
			dst:  []string{},
			src:  []byte{0x01, 0x02, 0x03},
			want: 3,
		},
		{
			name: "four bytes",
			dst:  []string{},
			src:  []byte{0x01, 0x02, 0x03, 0x04},
			want: 3,
		},
		{
			name: "eight bytes",
			dst:  []string{},
			src:  []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
			want: 6,
		},
		{
			name: "with existing dst",
			dst:  []string{"existing"},
			src:  []byte{0x01},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EncodeWordList(tt.dst, tt.src)
			if len(result) != tt.want {
				t.Errorf("EncodeWordList() returned %d words, want %d", len(result), tt.want)
			}
			
			// Check that all words are valid
			for i, word := range result {
				if word == "" {
					t.Errorf("EncodeWordList() returned empty word at index %d", i)
				}
			}
		})
	}
}

func TestEncodeWordListConsistency(t *testing.T) {
	input := []byte{0x12, 0x34, 0x56, 0x78}
	
	// Encode twice with empty dst
	result1 := EncodeWordList([]string{}, input)
	result2 := EncodeWordList([]string{}, input)
	
	if len(result1) != len(result2) {
		t.Errorf("Inconsistent result lengths: %d vs %d", len(result1), len(result2))
	}
	
	for i := range result1 {
		if result1[i] != result2[i] {
			t.Errorf("Inconsistent result at index %d: %s vs %s", i, result1[i], result2[i])
		}
	}
}

func TestEncodeWordListCapacityHandling(t *testing.T) {
	// Test with dst that has sufficient capacity
	dst := make([]string, 1, 10)
	dst[0] = "existing"
	input := []byte{0x01, 0x02}
	
	result := EncodeWordList(dst, input)
	
	if len(result) != 3 { // 1 existing + 2 new
		t.Errorf("Expected 3 words, got %d", len(result))
	}
	
	if result[0] != "existing" {
		t.Errorf("Expected first word to be 'existing', got %s", result[0])
	}
}

func TestEncodeWordListBoundaryValues(t *testing.T) {
	tests := []struct {
		name string
		src  []byte
	}{
		{"max single byte", []byte{0xFF}},
		{"max two bytes", []byte{0xFF, 0xFF}},
		{"max three bytes", []byte{0xFF, 0xFF, 0xFF}},
		{"max four bytes", []byte{0xFF, 0xFF, 0xFF, 0xFF}},
		{"all zeros", []byte{0x00, 0x00, 0x00, 0x00}},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EncodeWordList([]string{}, tt.src)
			expectedLen := WordsRequired(len(tt.src))
			
			if len(result) != expectedLen {
				t.Errorf("Expected %d words, got %d", expectedLen, len(result))
			}
			
			// Ensure all words are from the WordList
			for _, word := range result {
				found := false
				for _, validWord := range WordList {
					if word == validWord {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Invalid word generated: %s", word)
				}
			}
		})
	}
}