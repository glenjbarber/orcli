package tui

import "fmt"

// promptRow is what the prompt row carries, the prompt and the reader's field.
//
// The two are cut together rather than separately, since a field longer than the
// terminal has no end to show and the reader is typing at the end of it.
//
// CutColumn cuts from the front and reports what it dropped, and the dropped figure is
// what placeCaret needs: the caret column is measured against what is on the row, so a
// field scrolled off the left has to be subtracted from it or the caret lands early.
func (l *interfaceLoop) promptRow() string {
	width := l.screen.Width()
	if width < minSize {
		width = minSize
	}

	text, _ := CutColumn(Prompt+l.editor.Text(), width)
	return text
}

// placeCaret puts the terminal cursor on the prompt row, at the caret.
//
// It is called on every paint because every other thing that writes in this frame can
// leave the cursor somewhere else, and a reader typing into a caret they cannot see
// is the failure this avoids.
//
// The column is counted in display columns and not in runes or bytes, since the
// prompt is ASCII and the field is not, and a caret placed by a byte count lands in
// the middle of a glyph. The text before the caret is measured rather than the caret's
// index, since a field holding a wide character is two columns per character and an
// index is not a column.
func (l *interfaceLoop) placeCaret() {
	width := l.screen.Width()
	if width < minSize {
		width = minSize
	}

	runes := []rune(l.editor.Text())
	before := string(runes[:l.editor.Caret()])

	// A caret past the right edge cannot be shown on a row that does not hold it,
	// so it is placed at the last column rather than past the end of the row, which
	// a terminal clamps to its own edge and which looks like a caret the reader
	// cannot account for.
	column := DisplayWidth(Prompt) + DisplayWidth(before)
	if column > width-1 {
		column = width - 1
	}
	if column < 0 {
		column = 0
	}

	l.screen.Write("\r")
	if column > 0 {
		l.screen.Write(fmt.Sprintf("\x1b[%dC", column))
	}
}
