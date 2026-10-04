package tui

import (
	"fmt"
	"strings"
	"testing"
)

// TestTheStackOrderIsTopToBottom is a diagnostic. It prints what DrawStack writes, in
// order, so a reader can see which row lands where rather than infer it from the slice.
func TestTheStackOrderIsTopToBottom(t *testing.T) {
	screen, out := drawnAt(24, 80)
	DrawStack(screen, Bar{
		Bottom:  "Provider: openrouter.ai",
		Top:     "Status: working",
		Field:   "THE FIELD",
		Twiddle: "THE TWIDDLE",
		Active:  "THE ACTIVE ROW",
		Tasks:   "THE TASK ROW",
	}, plainPalette())

	lines := stackRowsOnly(out.String())
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%d: %q\n", i, line)
	}
	t.Logf("DrawStack wrote, first to last:\n%v", b.String())
	t.Logf("the first line written lands at the TOP of the screen, the last at the BOTTOM")
}
