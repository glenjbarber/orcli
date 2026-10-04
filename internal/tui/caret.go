package tui

import (
	"fmt"
)

// escapeMoveUp moves the cursor up a row.
const escapeMoveUp = "\x1b[1A"

// escapeEraseLine clears the row the cursor is on.
//
// The row is cleared before it is written rather than the text being overwritten, since a
// question replaced by a shorter one would otherwise show the reader the tail of the
// question they just sent.
const escapeEraseLine = "\x1b[2K"

// fieldRow is what the prompt row carries after the prompt itself: the reader's own
// text, and nothing else.
//
// The text is cut on its own rather than with the prompt in front of it, so the string
// that reaches the terminal is what the reader typed and nothing the caret arithmetic has
// to subtract. The prompt is written by the painter in front of it.
func (l *interfaceLoop) fieldRow() string {
	width := l.screen.Width() - DisplayWidth(Prompt) - FieldIndent
	if width < minSize {
		width = minSize
	}

	text, _ := CutColumn(l.editor.Text(), width)
	return text
}

// promptRowsUp is how far the cursor is from the prompt row once the footer has been
// drawn.
//
// DrawStack writes the footer's rows downward, so when it has finished the cursor is one
// row below the last row it wrote, which is one row below the prompt row rather than the
// full height of the footer. Climbing the full height is what put the caret on the top bar
// two rows above the text the reader was typing into.
func promptRowsUp(height int) int {
	if n := len(stackRowsToKeep(height)) - 1; n > 0 {
		return n
	}
	return 0
}

// drawPromptRow rewrites the prompt row where it already is.
//
// It is what a keystroke uses. The prompt row is the last row of the footer, so a change
// to the reader's own text is rewritten in place rather than by drawing the whole footer
// again: a footer rewritten on every keystroke advances the screen by its own height each
// time, since it is terminal rows and not a pinned region.
//
// The row is cleared before it is written, since a question replaced by a shorter one
// would otherwise leave the tail of the old one on screen beside the new.
func (l *interfaceLoop) drawPromptRow(bar Bar, palette Palette) {
	for range promptRowsUp(l.screen.Height()) {
		l.screen.Write(escapeMoveUp)
	}
	l.screen.Write(escapeEraseLine + chromeLine(palette, promptRow(bar)) + "\r\n")
}

// placeCaret puts the terminal cursor on the prompt row, at the caret.
//
// It is called on every paint because every other thing that writes in this frame can
// leave the cursor somewhere else, and a reader typing into a caret they cannot see is
// the failure this avoids.
//
// Nothing is placed while a turn is running, since the prompt row is carrying the figure
// and there is no text to type into. Leaving the cursor where it is rather than moving it
// off the row is what keeps it from being visible on the bar below.
//
// The column is counted in display columns and not in runes or bytes, since the field is
// not ASCII and a caret placed by a byte count lands in the middle of a glyph. The text
// before the caret is measured rather than the caret's index, since a field holding a
// wide character is two columns per character and an index is not a column.
func (l *interfaceLoop) placeCaret(running bool) {
	// A row carrying the figure has nothing to type into, so no caret is placed and
	// the cursor is left wherever the last write put it.
	if running {
		return
	}

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
	column := FieldIndent + DisplayWidth(Prompt) + DisplayWidth(before)
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
