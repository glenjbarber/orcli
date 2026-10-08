package tui

import "testing"

// TestNewHistoryHasNoEntriesAndNoWalk covers the state a reader meets before
// anything has been sent.
func TestNewHistoryHasNoEntriesAndNoWalk(t *testing.T) {
	h := NewHistory()

	if got := h.Entries(); len(got) != 0 {
		t.Errorf("a new history has entries %v, want none", got)
	}
	if _, moved := h.Up(""); moved {
		t.Errorf("Up on an empty history moved")
	}
}

// TestRecordPutsTheNewestLineFirst covers the ordering adr-0000011 settled: most
// recent first, so the first Up reaches the reader's last line rather than their
// first.
func TestRecordPutsTheNewestLineFirst(t *testing.T) {
	h := NewHistory()
	h.Record("first")
	h.Record("second")
	h.Record("third")

	want := []string{"third", "second", "first"}
	got := h.Entries()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRecordDoesNotStoreAnEmptyLine covers the one line a reader cannot have sent:
// submit never sends an empty line, so there is nothing for Record to be asked to
// keep, but a caller that asked anyway should not get an empty row to reach for.
func TestRecordDoesNotStoreAnEmptyLine(t *testing.T) {
	h := NewHistory()
	h.Record("")

	if got := h.Entries(); len(got) != 0 {
		t.Errorf("recording an empty line gave entries %v, want none", got)
	}
}

// TestRecordDeduplicatesKeepingTheMostRecentPosition covers the decision adr-0000011
// gave the most care: three `ls` presses over an hour give one `ls`, at the top, not
// three rows nor the oldest of them.
func TestRecordDeduplicatesKeepingTheMostRecentPosition(t *testing.T) {
	h := NewHistory()
	h.Record("ls")
	h.Record("pwd")
	h.Record("ls")

	want := []string{"ls", "pwd"}
	got := h.Entries()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRecordDeduplicatesOnExactTextOnly covers the other half of that decision: two
// lines differing only in trailing whitespace are two entries, since trimming them
// would be a guess about what the reader meant.
func TestRecordDeduplicatesOnExactTextOnly(t *testing.T) {
	h := NewHistory()
	h.Record("ls")
	h.Record("ls ")

	want := []string{"ls ", "ls"}
	got := h.Entries()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// TestUpWalksOneEntryPerPressAndStopsAtTheOldest covers the walk itself: one entry
// per press, and a press at the oldest entry does nothing further rather than
// wrapping back to the newest.
func TestUpWalksOneEntryPerPressAndStopsAtTheOldest(t *testing.T) {
	h := NewHistory()
	h.Record("first")
	h.Record("second")

	text, moved := h.Up("")
	if !moved || text != "second" {
		t.Fatalf("first Up gave (%q, %v), want (%q, true)", text, moved, "second")
	}

	text, moved = h.Up(text)
	if !moved || text != "first" {
		t.Fatalf("second Up gave (%q, %v), want (%q, true)", text, moved, "first")
	}

	text, moved = h.Up(text)
	if moved {
		t.Fatalf("a third Up at the oldest entry moved, want no further movement")
	}
	if text != "first" {
		t.Fatalf("a third Up at the oldest entry returned %q, want it unchanged at %q", text, "first")
	}
}

// TestDownWithNoWalkDoesNothing covers Down pressed with nothing under way: there is
// nowhere closer to the present than the reader's own line already in the field.
func TestDownWithNoWalkDoesNothing(t *testing.T) {
	h := NewHistory()
	h.Record("first")

	text, moved := h.Down("whatever is typed")
	if moved {
		t.Errorf("Down with no walk in progress moved")
	}
	if text != "whatever is typed" {
		t.Errorf("Down with no walk in progress returned %q, want it unchanged", text)
	}
}

// TestDownRestoresTheDraftExactlyAfterWalkingBack covers the one case adr-0000011
// calls out by name: a reader who walks up three entries and back down finds exactly
// what they had, not an empty field.
func TestDownRestoresTheDraftExactlyAfterWalkingBack(t *testing.T) {
	h := NewHistory()
	h.Record("first")
	h.Record("second")

	draft := "a note half written"
	text, _ := h.Up(draft)
	if text != "second" {
		t.Fatalf("Up gave %q, want %q", text, "second")
	}

	text, moved := h.Down(text)
	if !moved {
		t.Fatalf("Down back past the only walked entry did not move")
	}
	if text != draft {
		t.Fatalf("Down restored %q, want the original draft %q", text, draft)
	}

	// A second Down with the walk already ended does nothing further.
	text, moved = h.Down(text)
	if moved {
		t.Errorf("Down after the walk already ended moved")
	}
	if text != draft {
		t.Errorf("Down after the walk already ended returned %q, want %q", text, draft)
	}
}

// TestUpThenDownThenUpAgainWalksCleanly covers a walk that goes up, comes all the way
// back to the draft, and goes up again, which should behave like a fresh walk rather
// than carrying over a stale cursor.
func TestUpThenDownThenUpAgainWalksCleanly(t *testing.T) {
	h := NewHistory()
	h.Record("first")
	h.Record("second")

	draft := "draft"
	text, _ := h.Up(draft)
	text, _ = h.Down(text)
	if text != draft {
		t.Fatalf("walking back to the draft gave %q, want %q", text, draft)
	}

	text, moved := h.Up(text)
	if !moved || text != "second" {
		t.Fatalf("a fresh Up gave (%q, %v), want (%q, true)", text, moved, "second")
	}
}

// TestRecordEndsAWalkInProgress covers sending a line partway through a walk: the
// next Up has to start a fresh walk against the newly recorded history rather than
// resuming the old cursor.
func TestRecordEndsAWalkInProgress(t *testing.T) {
	h := NewHistory()
	h.Record("first")
	h.Record("second")

	h.Up("draft")
	h.Record("third")

	text, moved := h.Up("")
	if !moved || text != "third" {
		t.Fatalf("Up after a Record gave (%q, %v), want (%q, true)", text, moved, "third")
	}
}
