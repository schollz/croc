// +build !windows

package uilive

import (
	"fmt"
)

func (w *Writer) clearLines() {
	for i := 0; i < w.lineCount; i++ {
		fmt.Fprintf(w.Out, "%c[2K", ESC)     // clear the line
		fmt.Fprintf(w.Out, "%c[%dA", ESC, 1) // move the cursor up
	}
}
