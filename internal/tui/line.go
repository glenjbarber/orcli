package tui

import (
	"fmt"
	"strings"
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

	// block is the one pasted block the line may hold, or nil when nothing
	// pasted is being tracked.
	//
	// v1 scope: at most one tracked block per line, inserted where the caret
	// was at the moment of the paste that created it. A reader can still
	// paste a second multi-line block on the same line, type before or after
	// the first, and so on; that is not refused. What is scoped out is
	// tracking more than one block at a time as a distinct, re-expandable
	// unit: a second multi-line paste simply drops the first block's
	// tracking (see InsertPastedText) and starts tracking the new one, and
	// any hand-edit that reaches inside a tracked block's own text drops
	// tracking for it too (see invalidateBlock). In both cases the text
	// already in the field is left exactly as it stood; only the ability to
	// toggle it back is given up.
	block *pastedBlock

	completion *completionCycle
}

type completionCycle struct {
	candidates []string
	index      int
	expected   string
	caret      int
}

// pastedBlock is the real text behind a collapsed paste summary.
//
// The Editor's own text always holds what is currently shown, either the
// summary runes or the raw runes, starting at start; block exists so a
// caller can tell which of the two is there now, get the real text back for
// submission, and flip between them.
type pastedBlock struct {
	// start is the rune index into the Editor's text where the block's
	// current form (summary or raw) begins.
	start int

	// raw is the real, possibly multi-line text the reader pasted.
	raw []rune

	// collapsed reports whether the field currently shows the summary
	// (true) or the raw text (false).
	collapsed bool
}

// pastedSummary is the one-line placeholder a collapsed block shows, e.g.
// "```pasted, 12 lines```".
func pastedSummary(lines int) string {
	return fmt.Sprintf("```pasted, %d lines```", lines)
}

// countLines reports how many lines raw is made of, counting the text
// before the first newline as line one.
func countLines(raw []rune) int {
	n := 1
	for _, r := range raw {
		if r == '\n' {
			n++
		}
	}
	return n
}

// currentLen is how many runes of the Editor's text the block occupies right
// now: the summary's length while collapsed, the raw text's length while
// expanded.
func (b *pastedBlock) currentLen() int {
	if b.collapsed {
		return len([]rune(pastedSummary(countLines(b.raw))))
	}
	return len(b.raw)
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
	e.block = nil
	e.completion = nil
}

// Insert puts a rune in at the caret and steps over it.
func (e *Editor) Insert(r rune) {
	e.beforeEdit(e.caret, 0)
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
	e.beforeEdit(e.caret-1, 1)
	e.text = append(e.text[:e.caret-1], e.text[e.caret:]...)
	e.caret--
}

// Delete removes the rune after the caret, and does nothing at the end.
func (e *Editor) Delete() {
	if e.caret >= len(e.text) {
		return
	}
	e.beforeEdit(e.caret, 1)
	e.text = append(e.text[:e.caret], e.text[e.caret+1:]...)
}

// beforeEdit is called before a single-rune insertion or removal at pos
// (removing n runes, 0 for an insertion), so the one tracked pasted block
// can be shifted when the edit lands outside it, or dropped when the edit
// reaches inside it.
//
// A hand-edit that reaches inside a block's own runes makes those runes no
// longer exactly the summary or exactly the raw text, so there is nothing
// left to toggle: the block is dropped and the runes already in the field
// are left exactly as the edit leaves them, now untracked plain text.
func (e *Editor) beforeEdit(pos, n int) {
	if e.block == nil {
		return
	}
	start := e.block.start
	end := start + e.block.currentLen()

	switch {
	case pos+n <= start:
		e.block.start -= n
	case pos >= end:
		// Entirely after the block: nothing to adjust.
	default:
		e.invalidateBlock()
	}
}

// invalidateBlock drops tracking of the one pasted block, leaving whatever
// runes are currently in the field exactly where they are.
func (e *Editor) invalidateBlock() { e.block = nil }

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
	e.invalidateBlock()
	e.text = append([]rune(nil), e.text[e.caret:]...)
	e.caret = 0
}

// ClearRight removes everything after the caret.
func (e *Editor) ClearRight() {
	e.invalidateBlock()
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
	e.invalidateBlock()
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
	e.invalidateBlock()
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
		e.completion = nil
		return
	}
	contextBefore := string(e.text[:start])
	if cycle := e.completion; cycle != nil && string(e.text) == cycle.expected && e.caret == cycle.caret {
		cycle.index = (cycle.index + 1) % len(cycle.candidates)
		e.applyCompletion(start, word, cycle.candidates[cycle.index], false)
		cycle.expected = string(e.text)
		cycle.caret = e.caret
		return
	}

	text, caret, whole, candidates := CompletionAt(contextBefore, word)
	if len(candidates) == 0 {
		e.completion = nil
		return
	}

	e.invalidateBlock()
	before := e.text[:start]
	after := e.text[e.wordEnd(start):]
	if len(candidates) > 1 {
		text = candidates[0]
		caret = len([]rune(text))
		whole = false
	}
	if whole {
		text += " "
		caret++
	}

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
	if len(candidates) > 1 {
		e.completion = &completionCycle{candidates: candidates, index: 0, expected: string(e.text), caret: e.caret}
	} else {
		e.completion = nil
	}
}

func (e *Editor) applyCompletion(start int, word, candidate string, whole bool) {
	e.invalidateBlock()
	before := e.text[:start]
	after := e.text[e.wordEnd(start):]
	text := candidate
	if whole {
		text += " "
	}
	runes := append(append([]rune(nil), before...), []rune(text)...)
	runes = append(runes, after...)
	e.text = runes
	e.caret = len(before) + len([]rune(text))
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

// InsertPastedText puts pasted text into the field at the caret.
//
// Pasted text is always literal content and never a submit trigger,
// whatever is in it: this is the one entry point SetPasteHandler calls, and
// it never turns an embedded '\r' or '\n' into a KeyEnter the way an earlier
// version of the paste handler did. That earlier behaviour is the bug this
// method exists to fix; see run.go's SetPasteHandler.
//
// A paste with no newline in it is unaffected: it carries nothing to
// collapse, so it is inserted exactly as plain typing would be, one rune at
// a time, which is also what single-line pasting did before this existed.
//
// A paste that does contain a newline is genuinely multi-line, and is
// collapsed to a one-line summary ("```pasted, N lines```") at the caret.
// The real text is kept, retrievable by SubmitText and by expanding the
// block with ToggleExpandPastedBlock; see the Editor.block field comment for
// the one-block-per-line scoping this takes in v1.
//
// Control runes other than the newlines themselves are dropped, matching
// what the paste handler filtered before this method existed.
func (e *Editor) InsertPastedText(text string) {
	raw := filterPastedRunes(text)
	if !containsNewline(raw) {
		for _, r := range raw {
			e.Insert(r)
		}
		return
	}

	// A second multi-line paste on a line that already has a tracked block
	// drops that block's tracking rather than refusing the new paste: the
	// runes already in the field from the first block are left exactly as
	// they stand (see the Editor.block field comment).
	e.invalidateBlock()

	start := e.caret
	for _, r := range []rune(pastedSummary(countLines(raw))) {
		e.Insert(r)
	}
	e.block = &pastedBlock{start: start, raw: raw, collapsed: true}
}

// ToggleExpandPastedBlock flips the one tracked pasted block between its
// collapsed summary and its real, raw text, and reports whether there was a
// block to flip.
//
// This is the expand/collapse mechanism DESIGN.md's paste-and-send
// separation calls for. Which key reaches it is decided in run.go's
// keyEvent/act, and is a placeholder there, not a confirmed binding.
func (e *Editor) ToggleExpandPastedBlock() bool {
	b := e.block
	if b == nil {
		return false
	}

	oldLen := b.currentLen()
	b.collapsed = !b.collapsed
	newRunes := []rune(pastedSummary(countLines(b.raw)))
	if !b.collapsed {
		newRunes = b.raw
	}

	before := e.text[:b.start]
	after := append([]rune(nil), e.text[b.start+oldLen:]...)
	e.text = append(append(append([]rune(nil), before...), newRunes...), after...)

	switch {
	case e.caret >= b.start+oldLen:
		e.caret += len(newRunes) - oldLen
	case e.caret > b.start:
		e.caret = b.start + len(newRunes)
	}

	return true
}

// HasPastedBlock reports whether the line is holding one pasted block,
// collapsed or expanded.
func (e Editor) HasPastedBlock() bool { return e.block != nil }

// PastedBlockCollapsed reports whether the one tracked pasted block is
// currently shown collapsed, and whether there is a block to report on.
func (e Editor) PastedBlockCollapsed() (collapsed, ok bool) {
	if e.block == nil {
		return false, false
	}
	return e.block.collapsed, true
}

// SubmitText returns the text this line actually submits: what the field
// shows, except that a still-collapsed pasted block is substituted back to
// its real, raw text first.
//
// Enter, History.Record and the runner all go through this rather than
// Text, so a reader who never expanded a collapsed block still has the real
// multi-line text sent and remembered, never the "```pasted, N lines```"
// placeholder (see submit in run.go).
func (e Editor) SubmitText() string {
	b := e.block
	if b == nil || !b.collapsed {
		return e.Text()
	}

	before := string(e.text[:b.start])
	after := string(e.text[b.start+b.currentLen():])
	return before + string(b.raw) + after
}

// filterPastedRunes drops control runes other than the newlines themselves,
// and normalizes "\r\n" and a lone "\r" to "\n", so raw is always plain text
// and lines joined by "\n" alone.
func filterPastedRunes(text string) []rune {
	runes := []rune(text)
	out := make([]rune, 0, len(runes))
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '\n':
			out = append(out, '\n')
		case '\r':
			out = append(out, '\n')
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
		default:
			if !unicode.IsControl(r) {
				out = append(out, r)
			}
		}
	}
	return out
}

// containsNewline reports whether raw holds an embedded line break, which is
// what makes a paste genuinely multi-line rather than a single line that
// merely arrived through the paste path.
func containsNewline(raw []rune) bool {
	return strings.ContainsRune(string(raw), '\n')
}
