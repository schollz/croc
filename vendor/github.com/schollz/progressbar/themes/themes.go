package themes

import (
	"errors"
)

var defaultSymbolsFinished = []rune("█◎▭☢≈⋮─━═=╸")
var defaultTheme = []rune("█ ||")

// New returns a new theme
func New(symbols ...string) []string {
	symbols = append(symbols, make([]string, 4-len(symbols))...)
	for i, _ := range symbols {
		if len(symbols[i]) == 0 {
			symbols[i] = string(defaultTheme[i])
		}
	}
	return []string{
		symbols[0],
		symbols[1],
		symbols[2],
		symbols[3],
	}
}

// NewDefault returns nth theme from default themes
func NewDefault(n uint8) ([]string, error) {
	if n > uint8(len(defaultSymbolsFinished)) {
		return nil, errors.New("n must be less than defined themes")
	}
	return New(string(defaultSymbolsFinished[n])), nil
}

func NewFromRunes(symbols []rune) ([]string, error) {
	if len(symbols) != 4 {
		return []string{}, errors.New("symbols lenght must be exactly 4")
	}
	return []string{
		string(symbols[0]),
		string(symbols[1]),
		string(symbols[2]),
		string(symbols[3]),
	}, nil
}
