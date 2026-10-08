package tui

// History is the record of every line a session has actually sent, and the
// walk a reader takes back through it with Up and Down.
//
// It exists because a reader reaching for something they typed an hour ago
// should not have to retype it, and because adr-0000011 settled a specific
// shape for that reach: most-recent-first, deduplicated on the exact text, and
// walked without sending or clearing anything. That record was itself marked
// superseded by loreloom/UI-redesign.md, but that document fixes only the
// login screen, the two status bars and the prompt string; it says nothing
// about editing keys or history and ends by leaving further design detail
// pending. So this is not the old record being revived: it is a fresh
// decision, for a part of the interface nothing has re-decided, that borrows
// the old record's reasoning because that reasoning still holds.
//
// # What goes in
//
// Every line that was sent, whatever it was. A question, a command and a
// pasted paragraph are all history, since a reader reaching back for what
// they wrote did not have to know which of the three it was. Record stores a
// line at the moment it is actually sent, which is the only moment this tree
// has: there is no message-queue mechanism built here yet, so there is no
// earlier moment of "queued" to distinguish from "sent".
//
// # Order and deduplication
//
// The history is ordered most recent first, and an entry that duplicates an
// earlier one is kept once, at the position of its most recent occurrence.
// Three `ls` presses over an hour give one `ls`, at the top: the reader
// pressing Up once reaches the thing they are most likely to want again,
// rather than the oldest of three identical rows. Deduplication is on the
// exact text; two lines differing only in trailing whitespace are two
// entries, since trimming them would be a guess about what the reader meant.
//
// # Walking
//
// Walking the history does not send anything and does not clear the line.
// Up and Down move a cursor over the stored entries and copy whichever one
// the cursor lands on into the field, one entry per press, stopping rather
// than wrapping at either end: a history that wrapped would send a reader
// looking for their first question on to their most recent one instead.
//
// The reader's own line, typed before the first Up of a walk, is not an
// entry in History. It belongs to the editor, and History only remembers it
// long enough to hand it back: Begin is called with that line when a walk
// starts, and Down past the newest entry returns it, restoring the field to
// what the reader had before they started walking rather than to the empty
// string.
type History struct {
	// entries holds the sent lines, most recent first.
	entries []string

	// cursor is the position in entries the last Up or Down landed on, or -1
	// when no walk is in progress. It resets to -1 on every Record, since a
	// newly sent line ends whatever walk was under way the way pressing
	// Enter always has.
	cursor int

	// draft is the reader's own line as it stood before the walk now under
	// way began. It is read back by Down when the walk returns past the
	// newest entry, and it is only meaningful while cursor is not -1.
	draft string
}

// NewHistory returns a history with nothing recorded and no walk in progress.
func NewHistory() History {
	return History{cursor: -1}
}

// Record adds a line that was just sent.
//
// It is called at the point a line is actually sent, whether it was typed
// and submitted straight away or held in some future queue and sent later:
// either way, Record runs once, when sending happens, so the history holds
// what the model was actually asked rather than what the reader once typed.
//
// An earlier occurrence of the same exact text is removed first, so the line
// ends up once, at the front, rather than twice with a stale copy further
// back. An empty line is not recorded, since there is nothing there for a
// reader to reach for again.
//
// Recording ends whatever walk was in progress. A line just sent is a line
// the editor is now empty for, and a cursor left pointing into the history
// would make the next Up resume a walk the reader has no reason to expect.
func (h *History) Record(line string) {
	if line == "" {
		return
	}

	for i, existing := range h.entries {
		if existing == line {
			h.entries = append(h.entries[:i], h.entries[i+1:]...)
			break
		}
	}
	h.entries = append([]string{line}, h.entries...)
	h.cursor = -1
	h.draft = ""
}

// Up walks one entry further back in the history and reports the text to
// show, and whether there was anywhere to go.
//
// current is the field as it stands right now. The first Up of a walk takes
// it as the draft to restore later, and every Up after that leaves the
// stored draft alone, since it already holds what the reader had before the
// walk began and current by then is only a history entry Up itself put
// there.
//
// Up at the oldest entry does nothing further: it reports the same text
// again rather than wrapping or re-fetching, so a reader who presses Up one
// time too many sees nothing move rather than being sent somewhere else.
func (h *History) Up(current string) (string, bool) {
	if len(h.entries) == 0 {
		return current, false
	}

	if h.cursor == -1 {
		h.draft = current
		h.cursor = 0
		return h.entries[0], true
	}

	if h.cursor >= len(h.entries)-1 {
		return current, false
	}
	h.cursor++
	return h.entries[h.cursor], true
}

// Down walks one entry back toward the present and reports the text to show,
// and whether there was anywhere to go.
//
// Down with no walk in progress does nothing: there is nowhere closer to the
// present than the reader's own unwalked line, which is presumably already
// in the field. Down that steps back past the newest entry restores the
// draft recorded when the walk began, exactly, and ends the walk, so a
// reader who went up three entries and came back down three finds precisely
// what they had, not an empty field.
func (h *History) Down(current string) (string, bool) {
	if h.cursor == -1 {
		return current, false
	}

	if h.cursor == 0 {
		h.cursor = -1
		draft := h.draft
		h.draft = ""
		return draft, true
	}
	h.cursor--
	return h.entries[h.cursor], true
}

// Entries returns the stored history, most recent first.
//
// It exists for tests and for a caller that wants to show or save the whole
// list; Up and Down never need it, since they carry the cursor themselves.
func (h History) Entries() []string {
	out := make([]string, len(h.entries))
	copy(out, h.entries)
	return out
}
