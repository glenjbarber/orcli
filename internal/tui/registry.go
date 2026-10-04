package tui

import (
	"errors"
	"sync"
)

// # Workers are reachable by a line, not by a process
//
// A worker holds its own conversation, its own level and its own goroutine, and
// none of those is reachable from outside the process. What reaches one is a line
// the reader types: the loop reads the key, the dispatcher resolves the name, and
// the session looks the worker up among the ones it holds. There is no other
// path, and that is the whole of how a status command finds a worker.
//
// The consequence is worth stating because it is easy to get wrong from the other
// side. A worker cannot be woken, queried or steered by anything outside this
// program. A reader who wants to know what one is doing types at it, and a caller
// in another process cannot do it at all. Anything that appears to reach a worker
// from outside is reaching the session, and the session is the only thing here
// that holds one.

// ErrNoWorker reports that nothing is running to answer.
//
// It is a refusal rather than a silent empty answer: a status command that prints
// nothing is a status command a reader cannot tell from a program that did not
// hear them.
var ErrNoWorker = errors.New("nothing is running")

// Running returns what is running, and is the answer a status command reports.
//
// It is a copy rather than the live list, since a worker finishes on its own
// goroutine and a caller reading the slice while it is written is a race.
func (s *Session) Running() []Level {
	var out []Level
	for _, w := range s.Workers() {
		if s.WorkerStateOf(w) == WorkerRunning {
			out = append(out, w.Level)
		}
	}
	return out
}

// RunningAt returns the worker running at a level, and whether one is.
//
// It is the lookup a line needs: a reader who typed a number reaches that worker
// and no other. A number naming a worker which has finished is reported as
// absent rather than as a failure, since a reader asking about a finished worker
// and a reader who mistyped are two different situations and a status command has
// to be able to tell them apart without refusing.
func (s *Session) RunningAt(level int) (*Worker, bool) {
	for _, w := range s.Workers() {
		if w.Level.Number != level {
			continue
		}
		if s.WorkerStateOf(w) != WorkerRunning {
			return nil, false
		}
		return w, true
	}
	return nil, false
}

// group counts the goroutines writing to the frame, and is what leaving waits
// for.
//
// It exists because returning to the shell with a request still in flight is a
// goroutine writing to a terminal that has been handed back. The count is raised
// before a goroutine starts and lowered after its answer has been drawn, so a
// wait that returns means the work finished rather than merely being cancelled.
//
// The counter is refused once Close has begun waiting, since a counter raised then
// is a counter nobody is waiting for and the reader would be handed a terminal
// with a turn still running in it.
type group struct {
	mu     sync.Mutex
	cond   *sync.Cond
	count  int
	closed bool
}

// newGroup returns a group holding nothing.
func newGroup() *group {
	g := &group{}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// Add raises the count, and refuses once Close has begun.
//
// The refusal is returned rather than absorbed: a caller that launched a turn and
// was refused has a turn nobody is waiting for, which is the failure this exists
// to prevent, so it has to be told.
func (g *group) Add() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return ErrClosing
	}
	g.count++
	return nil
}

// Done lowers the count and wakes anything waiting.
//
// It does nothing at zero rather than going negative. A Done that arrived before
// its Add would otherwise be lost and the Add would leave a count that nothing
// ever lowers, which is a wait that returns never: the reader quits and the
// terminal is handed back with a turn still counted.
func (g *group) Done() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.count > 0 {
		g.count--
	}
	if g.count == 0 {
		g.cond.Broadcast()
	}
}

// Close refuses further work and waits for what is counted.
//
// The wait is what makes leaving safe. It is not a sleep and not a poll: a
// goroutine that has not drawn its answer yet is one whose write lands on a
// terminal the reader has been given back.
//
// It waits on the condition rather than reading the count in a loop, so a turn
// that finishes while the wait is parked wakes it rather than making it spin.
func (g *group) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.closed = true
	g.cond.Broadcast()

	for g.count > 0 {
		g.cond.Wait()
	}
	return nil
}

// ErrClosing reports work refused because the session is leaving.
//
// It is named so a caller can tell this refusal from a fault in the work itself,
// since a turn refused for this reason never started and a turn refused for any
// other reason did.
var ErrClosing = errors.New("the session is closing")
