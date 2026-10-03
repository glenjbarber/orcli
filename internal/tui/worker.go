package tui

import (
	"errors"
	"strings"
)

// A worker is a background turn that holds a level of its own.
//
// # Why a worker is a level
//
// A worker is already a thread: it carries its own conversation, keeps its own log
// rather than writing into the main one, and outlives the turn that started it. The
// level is what makes `/copy N` reach it. Without one, a worker is background work
// the reader can watch in the bar but cannot copy out of, and a worker whose findings
// have to be selected by hand is a worker whose findings do not get used.
//
// The level is allocated when the worker is started rather than when it is asked
// anything, which is the rule levels.go already sets. A reader who walks away from a
// worker leaves a number spent and visible, rather than a number that could be handed
// out twice and point a handle at the wrong exchange.
//
// # A worker is refused in cognito
//
// Cognito promises that nothing is recorded. A worker acts on the host: it runs
// programs and writes files, and a file it wrote is a record of what the model did
// even though the transcript records nothing. So a session that promised nothing
// recorded does not start one. A reader who wants a worker has turned cognito off, and
// that is the same answer as refusing to approve the directory it would run in.

// Worker is one background turn, and the level it was given.
//
// It is a value rather than a pointer so that the level, the question and the state
// cannot disagree, and it carries a cancel because a worker a reader cannot stop is a
// worker that keeps spending the allowance after they have walked away.
type Worker struct {
	// Level is the identity `/copy N` takes for this worker.
	Level Level

	// Question is what the worker was asked. It is held rather than read back out of
	// the log, so that the level carries its own title: a reader who comes back to a
	// retired level long after the row has scrolled past can still be told what it
	// was.
	Question string

	// state is what the worker is doing. It is under the session lock rather than
	// held here unguarded, since a reader asks about it from the input goroutine
	// while the worker's own goroutine is finishing it.
	state WorkerState

	// cancel ends the worker's request. It is set once, at the start, and read from
	// the input goroutine when the reader stops it.
	cancel func()
}

// WorkerState is what a worker is doing, and it is separate from the session's.
//
// A session state describes the reader's own turn, and a worker running behind it
// does not change what the reader is in. Folding the two together would mean a
// background worker making the interface look busy, which is a lie about the reader's
// own turn and is the reason the two are separate values.
type WorkerState string

const (
	// WorkerRunning is a worker that has been started and has not finished.
	WorkerRunning WorkerState = "running"
	// WorkerDone is a worker that finished on its own.
	WorkerDone WorkerState = "done"
	// WorkerStopped is a worker the reader stopped, or one that failed. It is one
	// value rather than two because a reader who stopped a worker and a worker that
	// broke are both a worker that is not running, and the difference belongs in the
	// row that said why rather than in a state a reader scans.
	WorkerStopped WorkerState = "stopped"
)

// Spawn starts a worker on a level of its own, branching from parent.
//
// The parent is a parameter rather than the session's current level, since the
// conversation is what a worker branches from and a reader who is deep in a `/btw`
// wants the worker to inherit that prefix rather than the root.
//
// A question is required. A worker with nothing to do is a program started for no
// reason, and the reason it was started is the one thing the reader typed.
//
// Cognito is refused before the level is spent, so a refused spawn does not leave a
// hole in the numbering. A reader who turns cognito off and tries again gets the
// number they would have had, rather than one further along.
func (s *Session) Spawn(parent int, question string) (*Worker, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if s.opts.Cognito {
		return nil, ErrCognito
	}
	if question == "" {
		return nil, ErrNoQuestion
	}

	level, err := s.levels.open(parent, question)
	if err != nil {
		return nil, err
	}

	w := &Worker{Level: level, Question: question, state: WorkerRunning}

	s.mu.Lock()
	s.workers = append(s.workers, w)
	s.mu.Unlock()

	s.log.Append(Row{
		Kind:  KindQuestion,
		Level: level.Number,
		Text:  question,
		Spans: []Span{{Start: 0, End: len(question), Role: RoleEmphasis}},
	})
	return w, nil
}

// SpawnWithCancel starts a worker whose request stop closes.
//
// It is a second entry point rather than a field the caller sets, because the cancel
// has to be created before the worker runs or the first stop is a stop of nothing. A
// caller that could set it afterwards would have a window in which a worker is
// unstoppable, and a window in which a reader cannot stop a model is the same failure
// as one they never could.
func (s *Session) SpawnWithCancel(parent int, question string,
	cancel func()) (*Worker, error) {

	w, err := s.Spawn(parent, question)
	if err != nil {
		return nil, err
	}
	w.cancel = cancel
	return w, nil
}

// Workers returns the workers this session started, in the order they were started.
//
// The order is by start rather than by level, since a reader scanning the list is
// looking for the newest one and a list sorted by number puts the oldest last.
func (s *Session) Workers() []*Worker {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Worker, len(s.workers))
	copy(out, s.workers)
	return out
}

// WorkerAt returns the worker holding a level, and whether one does.
//
// A level and a worker are not the same thing, and the difference matters when a
// level was opened by something that is not a worker: a `/btw` has a level and is
// answered in the session rather than in the background. A reader typing a number
// gets the level either way; a caller asking for the worker gets one only when there
// is one.
func (s *Session) WorkerAt(level int) (*Worker, bool) {
	for _, w := range s.Workers() {
		if w.Level.Number == level {
			return w, true
		}
	}
	return nil, false
}

// WorkerStateOf reports what a worker is doing.
//
// A method on the session rather than on the worker, so a caller holding a worker
// does not hold a state it read at some earlier moment and cannot refresh.
func (s *Session) WorkerStateOf(w *Worker) WorkerState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return w.state
}

// Finish records that a worker ended and retires its level.
//
// The level is retired here rather than by the caller, because the two questions are
// the same question: a worker that has finished is a worker whose exchange has ended.
// A caller that had to remember both would eventually remember one.
//
// It is safe to call on a worker that has already finished. A reader who presses the
// key twice is asking for the same thing twice, and the second ask failing is a
// failure the reader did not cause.
func (s *Session) Finish(w *Worker, state WorkerState, reason string) {
	s.mu.Lock()
	already := w.state != WorkerRunning
	if !already {
		w.state = state
	}
	s.mu.Unlock()

	if already {
		return
	}

	s.log.Append(Row{
		Kind:  KindNotice,
		Level: w.Level.Number,
		Text:  reason,
		Spans: []Span{{Start: 0, End: len(reason), Role: RoleDim}},
	})
	_ = s.levels.retire(w.Level.Number)
}

// Stop ends a worker's request and retires its level.
//
// It is safe to call on a worker that has already finished, for the reason Finish
// gives: the reader asked twice and the second ask is not a fault.
func (s *Session) Stop(w *Worker, reason string) {
	if w.cancel != nil {
		w.cancel()
	}
	s.Finish(w, WorkerStopped, reason)
}

// ErrNoQuestion reports a spawn with nothing to ask.
//
// It is a named value rather than a string at each call site, so the two spellings
// cannot drift and a caller can test for it with errors.Is.
var ErrNoQuestion = errors.New("no question was given")

// ErrCognito reports a worker refused because nothing is to be recorded.
//
// It is the refusal `/spawn` has always made and for the same reason: a worker runs
// programs and writes files, and cognito promises that nothing is recorded.
var ErrCognito = errors.New("a worker acts on the host, and this session records nothing")

// CopyText renders what a level's rows would copy, as plain text.
//
// This is where `/copy N` gets its text from, and the shape of it is the decision
// rather than the encoding. Every row at the level, in the order they happened: a
// worker copied out of the session is a question and the answer to it, and a reader
// pasting that into a ticket wants both rather than an answer with no question
// attached.
//
// A row keeps its text and loses its styling, since a clipboard is plain text and a
// span is a thing this package means to itself. The credential is not in any row, so
// it is not here either, and that is the reason the log holds a plain-text contract
// rather than one that is only plain while nothing interesting has happened.
//
// A closed level still returns its text, alongside ErrLevelClosed. The reader gets
// their rows and learns the level is closed, which is a different situation from a
// number that was never handed out and gets nothing at all.
func (s *Session) CopyText(n int) (string, Level, error) {
	level, known := s.levels.lookup(n)
	if !known {
		return "", Level{}, ErrNoLevel
	}

	var b strings.Builder
	for _, row := range s.RowsAt(n) {
		b.WriteString(PlainRow(row).Text)
		b.WriteByte('\n')
	}

	if level.Closed {
		return b.String(), level, ErrLevelClosed
	}
	return b.String(), level, nil
}
