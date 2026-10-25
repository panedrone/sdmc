package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestPaint(t *testing.T) {
	if got := paint("a\n\nb"); got != "\x1b[31ma\x1b[0m\n\n\x1b[31mb\x1b[0m" {
		t.Errorf("%q", got)
	}
}

// not a terminal (a pipe, a file, CI): no escape sequences
func TestNoColorOffTerminal(t *testing.T) {
	var b bytes.Buffer
	printErr(&b, errors.New("x\ny"))
	if b.String() != "x\ny\n" {
		t.Errorf("%q", b.String())
	}
}
