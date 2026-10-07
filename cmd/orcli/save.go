package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/glenjbarber/orcli/internal/saved"
	"github.com/glenjbarber/orcli/internal/tui"
)

// ErrNoSaveName is returned when /save is typed with nothing after it.
//
// A save with no name is a save a reader cannot ask for back by the name they
// typed, since there would be none: the name is the whole of what tells /load
// which file to open, and refusing here is cheaper than writing a file the
// command table's own doc comment never promised a name for.
var ErrNoSaveName = errors.New("save: /save needs a name")

// ErrNoLoadName is returned when /load is typed with nothing after it.
var ErrNoLoadName = errors.New("load: /load needs a name")

// savedStore opens the store /save writes into and /load reads from.
//
// It is internal/saved.DefaultDir rather than config.TempDir, which is the
// directory /begin's handoff note goes through. The two are the same kind of
// store, opened the same way, holding the same shape of file, but for opposite
// reasons: a handoff note is removed the moment the load that reads it back is
// done, and a named save is the one thing here a reader asked this client to
// keep. Rooting /save and /load at the temp directory would put a conversation
// a reader typed a name for next to files this client deletes on its own, and
// a reader who later found the sessions directory empty would have no way to
// tell whether that was ever true.
func savedStore() (*saved.Store, error) {
	dir, err := saved.DefaultDir()
	if err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}
	return saved.Open(dir)
}

// save is the handler for /save.
//
// The whole log is written, not only the levels still open: a saved
// conversation a reader loads back is this client's one chance to show them
// everything that happened, and a save that quietly dropped a closed thread
// would be a save that answers a question the reader never asked it to.
//
// What does not survive a save, deliberately left for a later pass rather than
// guessed at here: the level each row was attributed to (every row loads back
// at level 0, so a restored conversation reads as one thread rather than the
// branches it was held in), and the spans that coloured it (the plain text
// comes back, the emphasis does not). Neither the levels table nor a span is a
// column this format's schema carries, and inventing one is a larger decision
// than this handler is the place to make.
func (d *dispatcher) save(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/save needs an open interface")
	}

	name := strings.TrimSpace(args)
	if name == "" {
		return tui.Result{}, ErrNoSaveName
	}

	store, err := savedStore()
	if err != nil {
		return tui.Result{}, err
	}

	toSave := saved.Session{
		Name:  name,
		Model: d.session.Options().Model,
		Turns: rowsToTurns(d.session.Log().Rows()),
	}

	path, err := store.Save(toSave)
	if err != nil {
		return tui.Result{}, fmt.Errorf("save: %w", err)
	}

	return tui.Result{
		Text: fmt.Sprintf("saved: wrote the conversation to %s", path),
	}, nil
}

// load is the handler for /load.
//
// It replaces the calling session's own log, which is what Session.Restore is
// for; it does not open a second session the way /begin does, since /begin is
// forking off a task for a worker to run and /load is the reader asking to see
// a conversation they already had, in the pane they are sitting at.
//
// The model travels with the save and is restored too, when the save carries
// one: a reader who loads a conversation held with a model other than the one
// this session happens to be set to should be answered the way that
// conversation was, not sent on in whatever model the pane was last pointed
// at. A save with no model recorded, which is every save a build before this
// one wrote, leaves the session's model exactly as it was.
func (d *dispatcher) load(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/load needs an open interface")
	}

	name := strings.TrimSpace(args)
	if name == "" {
		return tui.Result{}, ErrNoLoadName
	}

	store, err := savedStore()
	if err != nil {
		return tui.Result{}, err
	}

	loaded, err := store.Load(name)
	if err != nil {
		return tui.Result{}, fmt.Errorf("load: %w", err)
	}

	d.session.Restore(turnsToRows(loaded.Turns))
	if loaded.Model != "" {
		// The error this can return is ErrNoModel, for an empty model, and the
		// guard above already keeps an empty one from reaching here.
		_ = d.session.SetModel(loaded.Model)
	}

	return tui.Result{
		Text: fmt.Sprintf("loaded: resumed %s (%d rows)", name, len(loaded.Turns)),
	}, nil
}

// rowsToTurns renders a log's rows as the turns a Store writes.
//
// Content is the row's text, unchanged: a saved conversation is read with
// ordinary SQLite tools as much as it is read back by /load, and a column
// carrying an escaped or re-encoded copy of what the reader saw would be a
// column neither of those readers can trust. The kind becomes the role, by
// rowRole, which is the one piece of shape this conversion makes up: the
// schema's role column was built for a chat turn's role and a log's row is a
// wider idea than that, so a kind this client invented needs a name of its own
// in that column rather than being squeezed into "user" or "assistant" and
// mistaken for one.
func rowsToTurns(rows []tui.Row) []saved.Turn {
	if len(rows) == 0 {
		return nil
	}
	turns := make([]saved.Turn, len(rows))
	for i, row := range rows {
		turns[i] = saved.Turn{
			Role:    rowRole(row.Kind),
			Content: row.Text,
		}
	}
	return turns
}

// turnsToRows renders turns read back from a Store as the rows /load hands to
// Session.Restore.
//
// Every row loads back at level 0, since the level a row was attributed to is
// not one of the columns Save writes (see save's own doc comment for why), and
// a row with no level recorded is a row this client shows as belonging to the
// one thread every session starts with.
func turnsToRows(turns []saved.Turn) []tui.Row {
	if len(turns) == 0 {
		return nil
	}
	rows := make([]tui.Row, len(turns))
	for i, turn := range turns {
		rows[i] = tui.Row{
			Text: turn.Content,
			Kind: rowKind(turn.Role),
		}
	}
	return rows
}

// rowRole names a row's kind in the one word a save's role column holds.
//
// The names match RowKind's own identifiers rather than borrowing "user" and
// "assistant" from a chat turn, because a kind this client invented (a tool
// call, a queued prompt, a notice) is not any chat role and giving it one
// would be this conversion claiming a fact about the row that is not true.
func rowRole(k tui.RowKind) string {
	switch k {
	case tui.KindReply:
		return "reply"
	case tui.KindQuestion:
		return "question"
	case tui.KindTool:
		return "tool"
	case tui.KindQueued:
		return "queued"
	case tui.KindChrome:
		return "chrome"
	default:
		return "notice"
	}
}

// rowKind is rowRole's inverse.
//
// A role this client does not recognise, which is a file written by something
// else or by a later version that added a kind of its own, comes back as
// KindNotice rather than refusing the load: the row's text is still readable
// and a reader is better served by seeing it, marked as a notice, than by
// having the whole save refused for one row it cannot place.
func rowKind(role string) tui.RowKind {
	switch role {
	case "reply":
		return tui.KindReply
	case "question":
		return tui.KindQuestion
	case "tool":
		return tui.KindTool
	case "queued":
		return tui.KindQueued
	case "chrome":
		return tui.KindChrome
	default:
		return tui.KindNotice
	}
}
