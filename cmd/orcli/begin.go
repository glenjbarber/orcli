package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/saved"
	"github.com/glenjbarber/orcli/internal/tui"
)

// ErrNoNote is returned when /begin is typed with nothing after it.
//
// A fork with no task description is not a fork of anything, and refusing it here is
// cheaper than a new pane opening onto a note that says nothing.
var ErrNoNote = errors.New("begin: /begin needs a note describing the task")

const beginCheckInstruction = "Return the result to parent /pane 0 and check its work"

// begin is the handler for /begin.
//
// It implements staged/adr-0000047: the calling session is left exactly as it was,
// and a second, independent session is opened holding only the handoff note, not the
// calling session's transcript. The note travels through the same save-then-load
// mechanism /save and /load are built on (internal/saved), written to a file under
// orcli's own temp directory and removed the moment the load that reads it back is
// done, rather than through a pipe and rather than living on disk past the handoff.
//
// The new session is pane 1 and is held on the dispatcher as the one the next
// `/begin` replaces. Pane 0 remains the calling session; Ctrl+B navigation and
// `/pane 0` or `/pane 1` switch which session the interface displays.
func (d *dispatcher) begin(note string) (tui.Result, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return tui.Result{}, ErrNoNote
	}
	if !strings.HasSuffix(note, beginCheckInstruction) {
		note += "\n\n" + beginCheckInstruction
	}

	loaded, err := handoff(note)
	if err != nil {
		return tui.Result{}, err
	}

	worker := tui.New(d.session.Options())
	if err := worker.SetPane("1"); err != nil {
		return tui.Result{}, err
	}
	for _, turn := range loaded.Turns {
		worker.Notice(turn.Content, 0, tui.RoleEmphasis)
	}
	d.worker = worker

	return tui.Result{
		Text: fmt.Sprintf("begun: a new pane holds the handoff note (%d characters)", len(note)),
	}, nil
}

// handoff carries a handoff note through the save mechanism and back.
//
// It opens the same kind of store /save and /load open (internal/saved.Store), just
// rooted at orcli's dedicated temp directory rather than the sessions directory, so a
// handoff note is never confused for a session a reader asked to keep. The file this
// writes is removed before handoff returns, win or lose, so nothing from a /begin
// survives it on disk.
func handoff(note string) (saved.Session, error) {
	dir, err := config.TempDir()
	if err != nil {
		return saved.Session{}, fmt.Errorf("begin: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return saved.Session{}, fmt.Errorf("begin: create %s: %w", dir, err)
	}

	store, err := saved.Open(dir)
	if err != nil {
		return saved.Session{}, fmt.Errorf("begin: %w", err)
	}

	name, err := handoffName()
	if err != nil {
		return saved.Session{}, fmt.Errorf("begin: %w", err)
	}

	toSave := saved.Session{
		Name:  name,
		Turns: []saved.Turn{{Role: "user", Content: note}},
	}

	path, err := store.Save(toSave)
	if err != nil {
		return saved.Session{}, fmt.Errorf("begin: write the handoff note: %w", err)
	}
	// The file is removed on every path out from here, so a load that fails still
	// leaves nothing behind for a reader to find later and wonder about.
	defer os.Remove(path)

	loaded, err := store.Load(name)
	if err != nil {
		return saved.Session{}, fmt.Errorf("begin: read the handoff note back: %w", err)
	}
	return loaded, nil
}

// handoffName names the temporary file one handoff writes.
//
// It is random rather than fixed, since two /begin commands running close together
// write two files and each must be removed by the load that read it, not by whichever
// one happens to finish first.
func handoffName() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("choose a name: %w", err)
	}
	return "begin-" + hex.EncodeToString(b[:]), nil
}
