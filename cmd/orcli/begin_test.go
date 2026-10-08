package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// beginDispatcher is a dispatcher whose session is set, the way withSession wires it
// in openInterface, pointed at a fake HOME so the handoff never touches a real
// reader's ~/.orcli.
func beginDispatcher(t *testing.T) *dispatcher {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	d := newDispatcherFor(config.Config{})
	d.withSession(tui.New(tui.Options{Model: "some/model"}))
	return d
}

// TestBeginDeliversTheNoteToANewSession covers the seam itself: a note handed to
// /begin ends up read into the new session's log, through the save/load round trip
// rather than a pipe.
func TestBeginDeliversTheNoteToANewSession(t *testing.T) {
	d := beginDispatcher(t)

	if _, err := d.begin("take over the release checklist"); err != nil {
		t.Fatalf("begin: %v", err)
	}

	if d.worker == nil {
		t.Fatal("begin left no worker session")
	}
	if got := d.worker.Pane(); got != "1" {
		t.Fatalf("the first /begin pane is %q, want identity 1", got)
	}

	found := false
	wantNote := "take over the release checklist\n\n" + beginCheckInstruction
	for _, row := range d.worker.Log().Rows() {
		if row.Text == wantNote {
			found = true
		}
	}
	if !found {
		t.Fatalf("the worker's log does not carry the full instruction %q: %+v", wantNote, d.worker.Log().Rows())
	}
	if !strings.HasSuffix(wantNote, "and check its work") {
		t.Fatalf("the handoff does not end with the required phrase: %q", wantNote)
	}
	if !strings.Contains(wantNote, "parent /pane 0") {
		t.Fatalf("the handoff does not identify parent pane 0: %q", wantNote)
	}
}

func TestNavigatePaneSwitchesBetweenMainZeroAndBeginOne(t *testing.T) {
	d := beginDispatcher(t)
	main := d.mainSession
	if _, err := d.begin("work in pane one"); err != nil {
		t.Fatalf("begin: %v", err)
	}
	worker := d.worker

	if got := d.navigatePane(1); got != worker || d.session != worker || d.focusedPane != 1 {
		t.Fatal("next pane did not focus pane 1")
	}
	if got := d.navigatePane(-1); got != main || d.session != main || d.focusedPane != 0 {
		t.Fatal("previous pane did not focus pane 0")
	}
}

// TestBeginLeavesTheCallingSessionUntouched covers 0000032's fork rule: the pane that
// called /begin keeps whatever it was doing, and gets no copy of the note itself.
func TestBeginLeavesTheCallingSessionUntouched(t *testing.T) {
	d := beginDispatcher(t)
	before := len(d.session.Log().Rows())

	if _, err := d.begin("a second task"); err != nil {
		t.Fatalf("begin: %v", err)
	}

	after := len(d.session.Log().Rows())
	if after != before {
		t.Fatalf("the calling session's log changed: had %d rows, now has %d", before, after)
	}
}

// TestBeginRefusesAnEmptyNote covers the one way /begin can be typed with nothing to
// hand off: refused outright, rather than forking a pane onto an empty note.
func TestBeginRefusesAnEmptyNote(t *testing.T) {
	d := beginDispatcher(t)

	if _, err := d.begin("   "); err != ErrNoNote {
		t.Fatalf("begin(\"   \"): got %v, want ErrNoNote", err)
	}
	if d.worker != nil {
		t.Fatal("an empty note still opened a worker session")
	}
}

// TestBeginLeavesNoTempFileBehind covers the task's own requirement: nothing persists
// on disk once the handoff completes, win or lose.
func TestBeginLeavesNoTempFileBehind(t *testing.T) {
	d := beginDispatcher(t)

	if _, err := d.begin("clean up after yourself"); err != nil {
		t.Fatalf("begin: %v", err)
	}

	dir, err := config.TempDir()
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("the temp directory is not empty after /begin: %v", entries)
	}
}

// TestBeginUsesOrclisOwnTempDirectory covers the task's other requirement: the
// handoff is written under orcli's own configuration directory, not an OS-default
// temp path, and the directory is actually reached while the handoff is in flight.
func TestBeginUsesOrclisOwnTempDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	want, err := config.TempDir()
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if want != filepath.Join(home, ".orcli", "temp") {
		t.Fatalf("TempDir() = %s, want %s/.orcli/temp", want, home)
	}

	d := newDispatcherFor(config.Config{})
	d.withSession(tui.New(tui.Options{Model: "some/model"}))
	if _, err := d.begin("reach the dedicated directory"); err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the dedicated temp directory was never created: %v", err)
	}
}
