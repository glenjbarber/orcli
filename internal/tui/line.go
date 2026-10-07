package tui

import (
	"unicode"
)

// Editor is the text on the prompt row, and the caret in it.
//
// The editor is a value over the field and never a thing that writes to the
// terminal. Run asks it what the prompt row should say and the painter draws it,
// since an editor that wrote behind the painter would be two things writing to one
// place, and the frame's one rule is that the painter owns the bottom rows.
//
// It is a prompt row and nothing else. The prompt is the last row of the screen
// and never moves, so the caret is always on the last row and never in the middle
// of the log: the editor never scrolls and never redraws a row above itself.
type Editor struct {
	// text is the field as typed.
	text []rune

	// caret is the position in text, counted in runes rather than bytes.
	//
	// Runes rather than bytes because the caret is placed by the terminal and a
	// byte count places it inside a multi-byte character, which puts a caret in the
	// middle of a glyph and makes a reader typing anything but ASCII fight the
	// field.
	caret int
}

// NewEditor returns an empty editor.
func NewEditor() Editor { return Editor{} }

// Text returns the field as typed.
func (e Editor) Text() string { return string(e.text) }

// Caret returns where the caret is, in runes.
func (e Editor) Caret() int { return e.caret }

// Empty reports whether there is nothing typed.
func (e Editor) Empty() bool { return len(e.text) == 0 }

// Reset clears the field and puts the caret at the start.
//
// It is called after a line is submitted rather than leaving the old text in
// place for the next question to inherit. A field that carried the previous
// question would be a field the reader has to clear by hand, and clearing is a
// keypress they should not have to know they need.
func (e *Editor) Reset() {
	e.text = e.text[:0]
	e.caret = 0
}

// Insert puts a rune in at the caret and steps over it.
func (e *Editor) Insert(r rune) {
	e.text = append(e.text, 0)
	copy(e.text[e.caret+1:], e.text[e.caret:])
	e.text[e.caret] = r
	e.caret++
}

// Backspace removes the rune before the caret, and does nothing at the start.
//
// A caret at the start is the reader pressing a key that cannot mean anything
// there, and a field that lost a character it does not have is a field a reader
// has to notice and undo.
func (e *Editor) Backspace() {
	if e.caret == 0 {
		return
	}
	e.text = append(e.text[:e.caret-1], e.text[e.caret:]...)
	e.caret--
}

// Delete removes the rune after the caret, and does nothing at the end.
func (e *Editor) Delete() {
	if e.caret >= len(e.text) {
		return
	}
	e.text = append(e.text[:e.caret], e.text[e.caret+1:]...)
}

// Left steps the caret back, and does nothing at the start.
func (e *Editor) Left() {
	if e.caret > 0 {
		e.caret--
	}
}

// Right steps the caret forward, and does nothing at the end.
func (e *Editor) Right() {
	if e.caret < len(e.text) {
		e.caret++
	}
}

// Home puts the caret at the start.
func (e *Editor) Home() { e.caret = 0 }

// End puts the caret at the end.
func (e *Editor) End() { e.caret = len(e.text) }

// ClearLeft removes everything before the caret, for the key that removes a line
// the other way round.
func (e *Editor) ClearLeft() {
	e.text = append([]rune(nil), e.text[e.caret:]...)
	e.caret = 0
}

// ClearRight removes everything after the caret.
func (e *Editor) ClearRight() {
	e.text = e.text[:e.caret]
}

// SetText replaces the whole field with text and puts the caret at its end.
//
// It is what walking history does to the field, and it is its own method
// rather than a Reset followed by a loop of Inserts, because a caller
// replacing the field is doing one thing rather than typing: there is no
// caret position partway through the arriving text that the walk should
// respect, and the only position worth naming afterwards is the end of it.
// The caret goes there because a reader who walks back to an old line
// resumes editing it from where they would have stopped typing it, not from
// its start, so pressing End or Left after Up lands where it ordinarily
// would.
func (e *Editor) SetText(text string) {
	e.text = []rune(text)
	e.caret = len(e.text)
}

// EraseWordBefore removes the word before the caret, for the emacs Ctrl+W
// spelling.
//
// The word is everything back from the caret to the previous run of
// non-space runes, mirroring how wordBeforeCaret finds the word Tab
// completes. The whitespace on both sides of that word, between it and
// whatever the caret is sitting against, goes with it: a caret already
// inside a run of spaces skips over them before finding a word to erase,
// and once the word is found the gap before it is removed too, so repeated
// presses walk back one word at a time with no stray double space left
// between what remains and what follows the caret.
func (e *Editor) EraseWordBefore() {
	end := e.caret
	start := end
	for start > 0 && unicode.IsSpace(e.text[start-1]) {
		start--
	}
	for start > 0 && !unicode.IsSpace(e.text[start-1]) {
		start--
	}
	for start > 0 && unicode.IsSpace(e.text[start-1]) {
		start--
	}

	e.text = append(e.text[:start], e.text[end:]...)
	e.caret = start
}

// Complete runs the completer over the field and puts what it answers in.
//
// It is the only completion this unit has, and it is the one the command table
// already answers: Completion returns the text and the caret, and the trailing
// space it puts after a whole name is what lets a reader type "/color " and then
// an argument rather than pressing tab before every word.
//
// A prefix that matches several names is left alone. The field is what the reader
// typed, and a completer that offered a choice would be a prompt this interface
// does not have.
func (e *Editor) Complete() {
	word, start := e.wordBeforeCaret()
	if word == "" {
		return
	}

	text, caret, whole := Completion(word)
	if !whole {
		return
	}

	before := e.text[:start]
	after := e.text[e.wordEnd(start):]

	// The caret is placed inside the completed word, which is where the completer
	// said it went, and then measured in runes again since the completion may have
	// changed the length of everything after it.
	runes := append(append([]rune(nil), before...), []rune(text)...)
	runes = append(runes, after...)

	e.text = runes
	e.caret = len(before) + caret
	if e.caret > len(e.text) {
		e.caret = len(e.text)
	}
}

// wordBeforeCaret returns the partial word the caret sits in, and where it starts
// in the rune slice.
//
// It is a rune index rather than a byte offset because the caret is a rune
// position, and a byte offset into the middle of a multi-byte character is a slice
// bound that panics rather than a position that is merely wrong.
func (e Editor) wordBeforeCaret() (string, int) {
	start := e.caret
	for start > 0 && !unicode.IsSpace(e.text[start-1]) {
		start--
	}
	return string(e.text[start:e.caret]), start
}

// wordEnd returns the index just past the word beginning at start.
//
// The word ends at the next space or at the end of the field, so the text after it
// survives a completion: a reader completing a name in the middle of a line keeps
// the argument they had already typed.
func (e Editor) wordEnd(start int) int {
	i := start
	for i < len(e.text) && !unicode.IsSpace(e.text[i]) {
		i++
	}
	return i
}
