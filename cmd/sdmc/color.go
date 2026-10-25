package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// isTerminal reports that w is a console, not a pipe, a file or CI.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// colored: escape sequences only to a terminal, and not with NO_COLOR (no-color.org).
func colored(w io.Writer) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok || !isTerminal(w) {
		return false
	}
	return enableColor(w.(*os.File))
}

// paint makes each line of s red: a terminal may reset the color at a line break.
func paint(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "\x1b[31m" + l + "\x1b[0m"
		}
	}
	return strings.Join(lines, "\n")
}

func red(w io.Writer, s string) string {
	if colored(w) {
		return paint(s)
	}
	return s
}

// printErr prints an error in red.
func printErr(w io.Writer, err error) {
	fmt.Fprintln(w, red(w, err.Error()))
}
