package tui

import "fmt"

// fieldRow is what the field row carries, which is the reader's own line and
// nothing else.
//
// There is no prompt in front of it any more. The field is cut on its own rather
// than with anything before it, so the text that reaches the terminal is what the
// reader typed, and the indent is applied by the painter so the caret and the row
// cannot disagree about where the line starts.
func (l *interfaceLoop) fieldRow() string {
	width := l.screen.Width() - FieldIndent
	if width < minSize {
		width = minSize
	}

	text, _ := CutColumn(l.editor.Text(), width)
	return text
}

// placeCaret puts the terminal cursor on the field row, at the caret.
//
// It is called on every paint because every other thing that writes in this frame can
// leave the cursor somewhere else, and a reader typing into a caret they cannot see
// is the failure this avoids.
//
// The column is counted in display columns and not in runes or bytes, since the field
// is not ASCII and a caret placed by a byte count lands in the middle of a glyph. The
// text before the caret is measured rather than the caret's index, since a field
// holding a wide character is two columns per character and an index is not a
// column.
//
// The indent is added, since the row is drawn at column five and a caret placed at
// the reader's own column within the text would be five columns to the left of where
// they are typing.
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
	column := FieldIndent + DisplayWidth(before)
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
