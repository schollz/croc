// +build windows

package uilive

import (
	"fmt"
	"github.com/mattn/go-isatty"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

var (
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procSetConsoleCursorPosition   = kernel32.NewProc("SetConsoleCursorPosition")
	procFillConsoleOutputCharacter = kernel32.NewProc("FillConsoleOutputCharacterW")
	procFillConsoleOutputAttribute = kernel32.NewProc("FillConsoleOutputAttribute")
)

type short int16
type dword uint32
type word uint16

type coord struct {
	x short
	y short
}

type smallRect struct {
	left   short
	top    short
	right  short
	bottom short
}

type consoleScreenBufferInfo struct {
	size              coord
	cursorPosition    coord
	attributes        word
	window            smallRect
	maximumWindowSize coord
}

func (w *Writer) clearLines() {
	f, ok := w.Out.(FdWriter)
	if ok && !isatty.IsTerminal(f.Fd()) {
		ok = false
	}
	if !ok {
		for i := 0; i < w.lineCount; i++ {
			fmt.Fprintf(w.Out, "%c[%dA", ESC, 0) // move the cursor up
			fmt.Fprintf(w.Out, "%c[2K\r", ESC)   // clear the line
		}
		return
	}
	fd := f.Fd()
	var csbi consoleScreenBufferInfo
	procGetConsoleScreenBufferInfo.Call(fd, uintptr(unsafe.Pointer(&csbi)))

	for i := 0; i < w.lineCount; i++ {
		// move the cursor up
		csbi.cursorPosition.y--
		procSetConsoleCursorPosition.Call(fd, uintptr(*(*int32)(unsafe.Pointer(&csbi.cursorPosition))))
		// clear the line
		cursor := coord{
			x: csbi.window.left,
			y: csbi.window.top + csbi.cursorPosition.y,
		}
		var count, w dword
		count = dword(csbi.size.x)
		procFillConsoleOutputCharacter.Call(fd, uintptr(' '), uintptr(count), *(*uintptr)(unsafe.Pointer(&cursor)), uintptr(unsafe.Pointer(&w)))
	}
}
