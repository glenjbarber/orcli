package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/glenjbarber/orcli/internal/tui"
)

// This file is the handlers for the level cluster: /pane, /spawn, /btw,
// /close, /copy, /delegate, /queue and /redirect. They all sit on the level
// and worker machinery internal/tui/session.go and internal/tui/worker.go
// already carry; nothing here invents a conversation branch or a background
// turn, since those already exist.
//
// # What this cluster does not do
//
// None of these commands actually runs a model turn at the level they open.
// Spawning a worker (internal/tui's own Spawn) and opening a thread
// (OpenLevel) are both, by their own doc comments and by retry.go's
// SpawnRetrying, the bookkeeping a background or branched exchange is built
// out of - the level number, the tombstone, the row recording what was
// asked - and not the exchange itself. Running one requires an AskFunc
// wired to a level, and the only AskFunc this program has (cmd/orcli/ask.go)
// is held by internal/tui's own interface loop, reached only through
// Result.Ask at level 0 (see run.go's start, hardcoded to level 0 for every
// command in this build, /spawn and /btw included).
//
// Wiring a second level through that path is a real plumbing change - a
// Result.Level field, a start that takes one, a submit that reads it - and
// doing it quietly inside a handler whose job the dispatch task described as
// "wire up... already-built primitives" would be building a new mechanism
// and calling it wiring. So this cluster stops at the primitive: a worker or
// a thread is opened, its question is written to its own level so `/copy`
// can reach it, and the reader is told the level number. Actually answering
// it is left to a /copy of that level fed back as an ordinary question, or
// to whatever lands the AskFunc plumbing this needs. This is flagged as a
// judgment call in the PR this change ships in, not asserted quietly here.

// panesUsage is what a /pane refusal names, so a reader told their argument
// is not a pane is told in the same breath what is.
const panesUsage = "main, delegate, spawn"

// pane is the handler for `/pane`.
//
// With no argument it reports the pane in force, the way /level and /model
// both do for the same reason: a reader who has never touched it should not
// have to change it just to find out what it is set to.
//
// There is one scrollback in this build regardless of what /pane says (see
// Session.SetPane's doc comment, and stack.go's renderPaneBar, which still
// draws the single hardcoded pane adr-0000007's multiplexer was never built
// to show more than one of). What /pane changes today is the name and the
// colour the pane bar draws, which is the one piece of the design this
// build can honor without that multiplexer.
func (d *dispatcher) pane(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/pane needs an open interface")
	}

	want := strings.TrimSpace(args)
	if want == "" {
		return tui.Result{Text: "the pane is " + d.session.Pane()}, nil
	}

	if err := d.session.SetPane(want); err != nil {
		return tui.Result{}, fmt.Errorf("/pane %s is not a pane; the panes are %s", want, panesUsage)
	}
	return tui.Result{Text: "the pane is " + want}, nil
}

// spawn is the handler for `/spawn`.
//
// The parent is always the root level, 0. There is no "current level"
// concept a reader occupies the way a shell occupies a directory - every
// command in this build runs at level 0 (see dispatch.go's Run and run.go's
// hardcoded level in start), and a reader who has branched into a thread
// with /btw is still typing commands at the top, not from inside the level
// they opened. Spawning from the one level every command already runs at is
// therefore the only "current level" this build has to offer, and is why
// Session.Spawn's own doc comment calls parent a parameter precisely so a
// caller like this one can hand it a different level once a reader can
// actually be in one; today there is no such reader, so it is 0.
//
// See this file's top-of-file comment for why the worker that comes back is
// registered and reported, not asked anything by this handler.
func (d *dispatcher) spawn(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/spawn needs an open interface")
	}

	question := strings.TrimSpace(args)
	if question == "" {
		return tui.Result{}, fmt.Errorf("/spawn needs a question")
	}

	w, err := d.session.Spawn(0, question)
	if err != nil {
		return tui.Result{}, err
	}
	return tui.Result{Text: fmt.Sprintf(
		"spawned a worker at level %d; /copy %d once it has something, /close %d when you are done with it",
		w.Level.Number, w.Level.Number, w.Level.Number)}, nil
}

// btw is the handler for `/btw`.
//
// It opens a level with OpenLevel, the primitive session.go's own doc
// comment names for exactly this: a thread branched from this conversation
// that a reader continues from later, by level number, rather than one that
// is answered right away. Unlike /spawn, nothing here refuses under cognito:
// OpenLevel allocates a level and nothing else, so there is no worker
// acting on the host for cognito's promise to be about.
//
// The title is taken from the question itself, cut to a length a reader
// skimming the level list (`/level`'s table has a parallel but unrelated
// idea of "level"; see Levels() for this one) can take in in one line,
// rather than left as the open level's whole text, which `/copy` already
// gives a reader who wants the rest of it.
//
// The question is written to the new level's own log, through Notice (the
// one row kind this package can append to an arbitrary level from outside
// internal/tui - see Session.Notice), so the level is not silently empty
// until something answers it: a reader who opens it later with /copy finds
// what the thread was started to ask, not nothing.
func (d *dispatcher) btw(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/btw needs an open interface")
	}

	question := strings.TrimSpace(args)
	if question == "" {
		return tui.Result{}, fmt.Errorf("/btw needs something to branch off about")
	}

	title := threadTitle(question)
	level, err := d.session.OpenLevel(0, title)
	if err != nil {
		return tui.Result{}, err
	}

	d.session.Notice(question, level.Number, tui.RoleEmphasis)
	return tui.Result{Text: fmt.Sprintf("opened level %d: %s", level.Number, title)}, nil
}

// delegate is the handler for `/delegate`.
//
// "ask a question alongside, without recording it" is, read against what
// this tree actually has, two different asks pulling in opposite
// directions: "alongside" wants a level of its own the way /spawn and /btw
// get one, and "without recording it" wants it to leave no row behind at
// all. Cognito (Options.Cognito) is the one "nothing is recorded" switch
// this build has, and it is session-wide and about a worker acting on the
// host (see worker.go's package doc comment), not a flag a single call can
// set for itself; there is no ask.go variant, and no Deliver/Notice
// parameter, that skips the log for one turn and not the rest.
//
// Given that, this ships /delegate as OpenLevel plus a Notice at the new
// level - the same shape as /btw, not /spawn, since nothing a question asked
// "alongside" needs acts on the host and cognito has no reason to refuse
// it. "Without recording" is read here as "without recording it into the
// main conversation" (level 0), which the level machinery already gives for
// free: the question lands at its own level, not level 0's. This is a
// genuine judgment call, named as one in the PR this lands in, and a
// narrower reading - a log entry nowhere at all - would need a new
// no-log path through Session that does not exist today.
func (d *dispatcher) delegate(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/delegate needs an open interface")
	}

	question := strings.TrimSpace(args)
	if question == "" {
		return tui.Result{}, fmt.Errorf("/delegate needs a question")
	}

	title := threadTitle(question)
	level, err := d.session.OpenLevel(0, title)
	if err != nil {
		return tui.Result{}, err
	}

	d.session.Notice(question, level.Number, tui.RoleEmphasis)
	return tui.Result{Text: fmt.Sprintf(
		"asked alongside at level %d, outside the main conversation: %s", level.Number, title)}, nil
}

// threadTitle turns a /btw or /delegate question into the level's title.
//
// Only the first line, since a question typed across several lines (a
// pasted block, say) would otherwise make a title that is itself several
// lines, and the level list shows one line per entry. Cut to threadTitleMax
// runes with an ellipsis past that, for the same reason: a title is read in
// the list, not in full, and the question itself is still there in full for
// `/copy` to return.
func threadTitle(question string) string {
	line := question
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)

	const threadTitleMax = 60
	runes := []rune(line)
	if len(runes) > threadTitleMax {
		return string(runes[:threadTitleMax]) + "…"
	}
	return line
}

// close is the handler for `/close`.
//
// It parses N and hands it straight to CloseLevel, which already tells a
// non-numeric argument apart from an out-of-range one by the error it
// returns: ErrNoLevel for a number nothing was ever handed out as, and
// ErrRootLevel for level 0, which CloseLevel's own doc comment says may
// never close since it is the conversation the reader is in. This handler
// adds only the third case neither of those covers: an argument that is not
// a number at all.
func (d *dispatcher) close(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/close needs an open interface")
	}

	want := strings.TrimSpace(args)
	if want == "" {
		return tui.Result{}, fmt.Errorf("/close needs a level number")
	}

	n, err := strconv.Atoi(want)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/close %s is not a level number", want)
	}

	if err := d.session.CloseLevel(n); err != nil {
		return tui.Result{}, err
	}
	return tui.Result{Text: fmt.Sprintf("closed level %d", n)}, nil
}

// copyCmd is the handler for `/copy`.
//
// It is named copyCmd rather than copy because copy is a builtin this
// package would otherwise shadow for every line below it, the same reason
// close above is close and not os.Close's name squatted on.
//
// CopyText, not CopyLevel, is what this calls: CopyText's own doc comment
// says it renders what a level's rows would copy, as plain text - exactly
// the shape a reader pasting `/copy`'s output into a ticket wants - where
// CopyLevel hands back the raw []Row internal/tui draws from, which is an
// internal shape this package has no business putting in a Result.Text.
//
// "N, or the whole conversation" is read as: level 0 is the whole
// conversation already, by name - newLevels's own doc comment calls level 0
// "the conversation" and levels.go's rootLevel constant is defined to be it -
// so a bare `/copy` with no argument copies level 0 rather than needing a
// second sentinel invented for this one command.
//
// A closed level still answers, carrying ErrLevelClosed; that is shown
// alongside the text rather than treated as a failure, since CopyText's own
// doc comment says a reader asking about a closed level should get their
// rows and learn it is closed, not be told there is nothing there.
func (d *dispatcher) copyCmd(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/copy needs an open interface")
	}

	want := strings.TrimSpace(args)
	n := 0
	if want != "" {
		parsed, err := strconv.Atoi(want)
		if err != nil {
			return tui.Result{}, fmt.Errorf("/copy %s is not a level number", want)
		}
		n = parsed
	}

	text, level, err := d.session.CopyText(n)
	if err != nil {
		if errors.Is(err, tui.ErrLevelClosed) {
			return tui.Result{Text: fmt.Sprintf("level %d (%s) is closed:\n%s", n, level.Title, text)}, nil
		}
		return tui.Result{}, err
	}
	return tui.Result{Text: text}, nil
}

// queue is the handler for `/queue`.
//
// There was no queue of any kind in this tree before this change - run.go's
// own status builder wrote fieldQueue as a literal 0 prompts, and its
// comment at the point this reads from now said so plainly. Session.Enqueue
// and Session.Drain are the new primitive this command needed built (see
// their doc comments in session.go), kept to the smallest shape that works:
// a FIFO slice, guarded by the lock every other per-session field already
// shares, drained by run.go's start once the turn ahead of it finishes
// clean. When exactly a queued prompt fires is this cluster's own judgment
// call, flagged where it is made, in run.go's start.
func (d *dispatcher) queue(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/queue needs an open interface")
	}

	text := strings.TrimSpace(args)
	if text == "" {
		return tui.Result{}, fmt.Errorf("/queue needs text to add")
	}

	d.session.Enqueue(text)
	return tui.Result{Text: fmt.Sprintf("queued (%d waiting): %s", d.session.QueueLen(), text)}, nil
}

// redirect is the handler for `/redirect`.
//
// The command table gives it no Args, unlike /spawn, /btw, /queue and
// /close, which is read as deliberate: /redirect takes nothing to say
// because what it interrupts is the turn already in flight, and what comes
// after is whatever the reader types next into the prompt it leaves empty
// and ready - the same prompt a plain Escape leaves behind (see run.go's
// stop), with a notice naming it a redirect rather than a stop so a reader
// scanning the log can tell "I interrupted this to move on" from "I gave up
// on this".
//
// StopTurn is the primitive both this and Escape sit on; this handler is the
// same two calls run.go's stop makes, reachable as a typed command for a
// reader who wants it recorded as that deliberate a choice rather than a key
// press, and refuses by name rather than silently doing nothing when there
// is no turn in flight to interrupt - Escape has no such complaint because
// a key press that did nothing leaves no trace to ask about, where a typed
// command the reader is waiting on an answer for does.
func (d *dispatcher) redirect(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/redirect needs an open interface")
	}

	cancel := d.session.StopTurn()
	if cancel == nil {
		return tui.Result{}, fmt.Errorf("/redirect: there is no turn in flight to redirect")
	}

	cancel()
	d.session.Finished("redirected")
	return tui.Result{Text: "the turn in flight was stopped; type what should run instead"}, nil
}
