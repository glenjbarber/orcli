package tui

import (
	"fmt"
	"strings"
	"testing"
)

// TestTheFrameOrderIsTopToBottom is a diagnostic. It prints what the frame writes, in order,
// so a reader can see which row lands where rather than infer it from the slice.
//
// It exists because the frame draws the log from the bottom up while the rows themselves go
// out from the top, which is two directions, and every layout fault this package has had came
// from confusing them. The log is gathered newest first and filled upward, so a row at the top
// of the screen is the oldest log row and a row just above the prompt is the newest.
func TestTheFrameOrderIsTopToBottom(t *testing.T) {
	screen, out := drawnAt(20, 80)

	var s Status
	s[fieldCwd] = "C"
	s[fieldSession] = "S"
	s[fieldState] = "STATE"
	log := []Row{
		{Text: "the oldest log row"},
		{Text: "a newer log row"},
		{Text: "the newest log row"},
	}

	DrawStack(screen, stackLines(Bar{Status: s, Field: "THE FIELD"}, log, 20, plainPalette()),
		plainPalette())

	rows := stackPositionedRows(out.String())

	var b strings.Builder
	for i, line := range rows {
		fmt.Fprintf(&b, "row %2d: %q\n", i+1, line)
	}
	t.Logf("the frame wrote %d rows:\n%v", len(rows), b.String())
	t.Logf("row 1 is the top of the screen, the log runs oldest to newest, " +
		"and the prompt is the last row")
}

// stackPositionedRows reads back the rows a frame write produced, in screen order.
//
// DrawStack writes, for each row, a positioning sequence, an erase, and the row text, so a
// row is the text following one erase sequence. The escapes are left in place because
// stripping them removes the terminator that marks where one row's writes end.
//
// The text of a row is everything after its erase up to the next positioning sequence, and
// the next positioning sequence is found by the row and column the frame wrote rather than by
// a search for a letter, since a row's own text can carry the same letter.
func stackPositionedRows(s string) []string {
	parts := strings.Split(s, escapeEraseLine)

	rows := make([]string, 0, len(parts))
	for i, p := range parts {
		// The first part is everything before the first erase, which is the positioning
		// sequence for row one and no text.
		if i == 0 {
			continue
		}

		// A part is the row's own text followed by the positioning sequence for the next
		// row, so the text is what precedes that sequence. The sequence is the last one in
		// the part and it is an escape, so the text is cut where the escape begins.
		if j := strings.LastIndex(p, "\x1b["); j >= 0 {
			p = p[:j]
		}
		rows = append(rows, strings.TrimRight(p, "\r\n"))
	}
	return rows
}
