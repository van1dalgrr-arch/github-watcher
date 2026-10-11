// Package logbuf — последние N строк лога демона в памяти, чтобы TUI мог показать их на вкладке Logs.
package logbuf

import (
	"strings"
	"sync"
)

type Ring struct {
	mu    sync.Mutex
	lines []string
	next  int
	full  bool
}

func New(size int) *Ring { return &Ring{lines: make([]string, size)} }

// Write реализует io.Writer: slog пишет по одной записи за вызов.
func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for l := range strings.SplitSeq(strings.TrimRight(string(p), "\n"), "\n") {
		r.lines[r.next] = l
		r.next = (r.next + 1) % len(r.lines)
		if r.next == 0 {
			r.full = true
		}
	}
	return len(p), nil
}

// Lines — строки от старых к новым.
func (r *Ring) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return append([]string(nil), r.lines[:r.next]...)
	}
	return append(append([]string(nil), r.lines[r.next:]...), r.lines[:r.next]...)
}
