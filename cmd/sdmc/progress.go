package main

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// status is one status line, rewritten via \r. Terminal only: in CI and in
// redirected output it writes nothing.
//
// From the first class on, a goroutine redraws the progress bar every tick, so that the
// time goes on while one slow class is generated; clear stops it.
type status struct {
	w     io.Writer
	title string        // of the progress bar: "generating", "validating"
	tick  time.Duration // how often the time is redrawn

	mu          sync.Mutex
	len         int // length of the last line, to blank it out with spaces
	start       time.Time
	done, total int
	class       string
	stop        chan struct{} // nil: no ticker
	stopped     chan struct{}
}

func newStatus(w io.Writer) *status {
	if isTerminal(w) {
		return &status{w: w, tick: time.Second}
	}
	return &status{}
}

func (s *status) show(format string, a ...any) {
	if s.w == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.showLocked(fmt.Sprintf(format, a...))
}

func (s *status) showLocked(line string) {
	n := utf8.RuneCountInString(line)
	pad := ""
	if s.len > n {
		pad = strings.Repeat(" ", s.len-n)
	}
	_, _ = fmt.Fprint(s.w, "\r"+line+pad)
	s.len = n
}

// clear stops the ticker and removes the line before regular output.
func (s *status) clear() {
	if s.w == nil {
		return
	}
	s.stopTicker()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearLocked()
}

// finish stops the ticker and leaves the progress bar on its own line, complete and with
// the time the whole run took. Without a progress bar (no classes) it is clear.
func (s *status) finish() {
	if s.w == nil {
		return
	}
	started := s.stopTicker()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !started {
		s.clearLocked()
		return
	}
	s.showLocked(progressLine(s.title, s.total, s.total, time.Since(s.start)))
	_, _ = fmt.Fprintln(s.w)
	s.len = 0
}

// keep leaves the current line on the screen: the next one starts below it.
func (s *status) keep() {
	if s.w == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.len > 0 {
		_, _ = fmt.Fprintln(s.w)
		s.len = 0
	}
}

// stopTicker stops the goroutine of the progress bar and waits for it; false if there was none.
func (s *status) stopTicker() bool {
	s.mu.Lock()
	stop, stopped := s.stop, s.stopped
	s.stop = nil
	s.mu.Unlock()
	if stop == nil {
		return false
	}
	close(stop)
	<-stopped
	return true
}

func (s *status) clearLocked() {
	if s.len > 0 {
		_, _ = fmt.Fprint(s.w, "\r"+strings.Repeat(" ", s.len)+"\r")
		s.len = 0
	}
}

// progress is called before the n-th class of total: n-1 are done. The time is counted
// from the first class, not from connecting.
func (s *status) progress(n, total int, class string) {
	if s.w == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop == nil {
		s.start = time.Now()
		s.stop, s.stopped = make(chan struct{}), make(chan struct{})
		go s.ticker(s.stop, s.stopped)
	}
	s.done, s.total, s.class = n-1, total, class
	s.redrawLocked()
}

func (s *status) ticker(stop, stopped chan struct{}) {
	defer close(stopped)
	t := time.NewTicker(s.tick)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.mu.Lock()
			s.redrawLocked()
			s.mu.Unlock()
		case <-stop:
			return
		}
	}
}

func (s *status) redrawLocked() {
	s.showLocked(progressLine(s.title, s.done, s.total, time.Since(s.start)) + " " + s.class)
}

const barWidth = 20

// progressLine: "generating [12/38] [▪▪▪▪▪▪          ] 31.58% (Elapsed: 00:00:04.20, ETA: 00:00:09.10)".
func progressLine(title string, done, total int, elapsed time.Duration) string {
	percent := 1.0
	if total > 0 {
		percent = min(float64(done)/float64(total), 1)
	}
	filled := int(percent * barWidth)
	if percent > 0 && filled == 0 {
		filled = 1
	}
	var eta time.Duration
	if done > 0 && done < total {
		eta = time.Duration(float64(elapsed) / float64(done) * float64(total-done))
	}
	return fmt.Sprintf("%s [%d/%d] [%s%s] %.2f%% (Elapsed: %s, ETA: %s)",
		title, done, total, strings.Repeat("▪", filled), strings.Repeat(" ", barWidth-filled),
		percent*100, hms(elapsed), hms(eta))
}

// hms formats a duration as hh:mm:ss.cc: with hundredths, as a fast run takes less than a second.
func hms(d time.Duration) string {
	cs := d.Milliseconds() / 10
	return fmt.Sprintf("%02d:%02d:%02d.%02d", cs/360000, cs/6000%60, cs/100%60, cs%100)
}

// dbLabel is where we connect, without login and password.
func dbLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	s := strings.TrimPrefix(raw, "jdbc:")
	if f, ok := strings.CutPrefix(s, "sqlite:"); ok {
		return f
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return u.Host + u.Path
	}
	return "the database"
}
