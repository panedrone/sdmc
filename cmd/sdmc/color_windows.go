package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableColor turns on escape sequences in the Windows console (conhost does not
// interpret them by default; Windows Terminal and IDE terminals do).
func enableColor(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
