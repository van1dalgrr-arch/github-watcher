package logbuf

import (
	"fmt"
	"slices"
	"testing"
)

func TestRing(t *testing.T) {
	r := New(3)
	_, _ = fmt.Fprintln(r, "a")
	_, _ = fmt.Fprintln(r, "b")
	if got := r.Lines(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("неполный буфер: %v", got)
	}
	_, _ = fmt.Fprint(r, "c\nd\n")
	if got := r.Lines(); !slices.Equal(got, []string{"b", "c", "d"}) {
		t.Fatalf("после переполнения: %v", got)
	}
}
