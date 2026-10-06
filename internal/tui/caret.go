package tui

import (
	"fmt"
	"strconv"
)

// The cursor, and the row of the frame that is redrawn where it stands.
//
// Both live here rather than in stack.go because both are about reaching the prompt row
// rather than about drawing the frame. The frame is written downward and leaves the cursor
// below it, so something has to say where the prompt row ended up. It is said by asking
// stack.go for the row rather than by counting rows back up here: a second figure for the
// same row is a second thing to be wrong, and the row arithmetic belongs beside the rows
// it describes.
//
// The caret never moves backwards. The prompt row is named and the column is reached by
// advancing forward from its left edge, so there is no sequence in this file that moves the
// cursor up through the frame.

// escapePosition places the cursor at a row and column, both counted from one.
//
// It is used rather than a sequence of relative moves because the prompt row's position
// depends on the frame's height and the frame may have been drawn at a different size, and
// a relative move from wherever the cursor happens to be is a position that is only right
// when nothing has moved since the last one.
func escapePosition(row, col int) string {
	return "\x1b[" + strconv.Itoa(row) + ";" + strconv.Itoa(col) + "H"
}

// escapeEraseLine clears the row the cursor is on.
//
// The row is cleared before it is written rather than the field being overwritten. A
// prompt row is as wide as the terminal and a shorter line written over a longer one
// leaves the tail of the old line visible, which a reader reads as text they did not type.
const escapeEraseLine = "\x1b[2K"

// fieldRow is what the prompt row carries after the prompt itself: the reader's own text,
// and nothing else.
//
// The text is cut on its own rather than with the prompt in front of it, so the string that
// reaches the terminal is what the reader typed and nothing the caret arithmetic has to
// subtract. The prompt is written by the painter in front of it.
func (l *interfaceLoop) fieldRow() string {
	width := l.screen.Width() - DisplayWidth(Prompt) - FieldIndent
	if width < minSize {
		width = minSize
	}

	text, _ := CutColumn(l.session.Editor().Text(), width)
	return text
}

// placeCaret puts the terminal cursor on the prompt row, at the caret.
//
// It is called on every paint because every other thing that writes in this frame can leave
// the cursor somewhere else, and a reader typing into a caret they cannot see is the
// failure this avoids.
//
// The column is counted in display columns and not in runes or bytes, since the field is
// not ASCII and a caret placed by a byte count lands in the middle of a glyph. The text
// before the caret is measured rather than the caret's index, since a field holding a wide
// character is two columns per character and an index is not a column.
//
// The row is addressed at column one and the column is reached by advancing, so the caret
// only ever moves forward from the left edge of the row it belongs to.
func (l *interfaceLoop) placeCaret() {
	width := l.screen.Width()
	if width < minSize {
		width = minSize
	}

	editor := l.session.Editor()
	runes := []rune(editor.Text())
	before := string(runes[:editor.Caret()])

	// A caret past the right edge cannot be shown on a row that does not hold it, so it is
	// placed at the last column rather than past the end of the row, which a terminal clamps
	// to its own edge and which looks like a caret the reader cannot account for.
	column := FieldIndent + DisplayWidth(Prompt) + DisplayWidth(before)
	if column > width-1 {
		column = width - 1
	}
	if column < 0 {
		column = 0
	}

	l.screen.Write(escapePosition(promptScreenRow(l.screen), 1))
	l.screen.Write("\r")
	if column > 0 {
		l.screen.Write(fmt.Sprintf("\x1b[%dC", column))
	}
}
