package tui

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Level is the identity a row is attributed to, and the handle `/copy N` takes.
//
// It is a thread identity rather than a pane identity, and the difference is the
// whole of why a row carries one. A pane could change while a turn ran, and a row
// already written would then be re-attributed to a responder it did not belong to,
// so a reader scanning back would find one exchange claimed by two. A level is
// fixed when the row is written and never moves.
//
// # Retired, not reused
//
// A level is retired rather than returned to a pool when its exchange ends. Reuse
// is what makes a handle lie: a reader who copied level 4 an hour ago and types
// `/copy 4` now would get whatever took the number, with nothing to say so. The
// cost is that the numbers grow without bound in a long session, and a reader who
// sees level 47 has to accept that 1 through 46 existed.
//
// A retired level keeps what it was, so `/copy 3` after 3 closed can say what 3
// was. That is the difference between a reader who mistyped and a reader who is
// behind, and a reader who cannot tell those two apart stops trusting the command.
//
// # A thread takes its own
//
// A `/btw` gets a level of its own rather than sharing its parent's. A thread is
// single-use, writes its findings, and ends itself, so the artifact it leaves on disk
// outlives the session that made it; a reader opening that file later has no gutter
// to read and no other way to know which question produced it. The parent
// relationship is a fact about the conversation rather than about the gutter.
type Level struct {
	// Number is what the reader sees and what `/copy N` takes.
	Number int

	// Parent is the level this one branched from, or -1 for a root level.
	//
	// It is recorded rather than shown, since the gutter shows identity and the
	// thread's parent is about where it came from. It is carried anyway because the
	// one place a reader is long past the gutter is a file the thread wrote.
	Parent int

	// Title is what the level was, for a reader who comes back to it later.
	Title string

	// Closed reports that the level has ended. A closed level is not offered for a
	// new thread and is reported as closed rather than absent.
	Closed bool
}

// rootParent is the parent of a level that has none.
//
// It is -1 rather than 0 because 0 is a real level, and a level whose parent reads as
// its own is a cycle in anything that walks the tree.
const rootParent = -1

// rootLevel is the conversation the reader is in.
//
// It is named because it is the one level a thread cannot branch from by accident and
// the one level that cannot be closed: a session whose own conversation is closed is a
// session with nowhere to type.
const rootLevel = 0

// levels is the table of levels a session has handed out.
//
// It is a map rather than a slice because the questions asked of it are by number:
// does this level exist, what was it, is it closed. The ordered view is produced on the
// way out for the bar, since a reader scanning a list does want it ordered and a
// writer asking about one level does not.
type levels struct {
	mu       sync.RWMutex
	byNumber map[int]Level
	next     int
}

// newLevels returns a table holding the root level and nothing else.
//
// Level 0 always exists, so `/copy 0` has something to copy from the first moment and a
// reader never meets a session where the conversation they are in has no number.
func newLevels() *levels {
	l := &levels{byNumber: make(map[int]Level), next: 1}
	l.byNumber[rootLevel] = Level{
		Number: rootLevel,
		Parent: rootParent,
		Title:  "the conversation",
	}
	return l
}

// open hands out a level for a thread branching from parent.
//
// The number is allocated here rather than at the moment the thread is asked, so that
// a level is spent even if the reader walks away from the thread before asking it
// anything. A number that could be reclaimed would be one that could be handed out
// twice, and a handle pointing at the wrong exchange is worse than a gap in the
// numbering that a reader can see.
//
// A reader who abandoned a thread therefore leaves a hole. That is the trade: the bar
// shows every level that was handed out, closed or not, so a hole is visible rather
// than mysterious.
func (l *levels) open(parent int, title string) (Level, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, known := l.byNumber[parent]; !known {
		return Level{}, fmt.Errorf("no level %d: %w", parent, ErrNoLevel)
	}

	level := Level{Number: l.next, Parent: parent, Title: title}
	l.byNumber[level.Number] = level
	l.next++
	return level, nil
}

// retire ends a level and keeps what it was.
//
// It is not removed. The tombstone is the point: a reader who asks about a retired
// level is told what it was rather than being told nothing, which is how a reader
// distinguishes a stale handle from a mistyped one.
//
// Retiring a level that is already closed is not an error, since a thread that ends
// itself and a reader who closes it afterwards is an ordinary sequence and refusing
// the second would make the reader's action the one that fails.
func (l *levels) retire(n int) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if n == rootLevel {
		return fmt.Errorf("level %d is the conversation: %w", n, ErrRootLevel)
	}

	level, known := l.byNumber[n]
	if !known {
		return fmt.Errorf("no level %d: %w", n, ErrNoLevel)
	}
	if level.Closed {
		return nil
	}

	level.Closed = true
	l.byNumber[n] = level
	return nil
}

// lookup returns a level and whether it was there.
func (l *levels) lookup(n int) (Level, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	level, ok := l.byNumber[n]
	return level, ok
}

// all returns every level handed out, ordered by number.
//
// Closed levels are included rather than filtered, since a reader typing a number
// wants to know what it was even after it closed, and a list that silently drops
// entries is how a reader concludes they mistyped.
func (l *levels) all() []Level {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make([]Level, 0, len(l.byNumber))
	for _, level := range l.byNumber {
		out = append(out, level)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// openLevels returns the levels still open, ordered by number.
//
// This is the list a new thread may branch from, which is why it is separate from all:
// a reader asking what a thread can branch from wants no closed entries in it.
func (l *levels) openLevels() []Level {
	var out []Level
	for _, level := range l.all() {
		if !level.Closed {
			out = append(out, level)
		}
	}
	return out
}

// ErrNoLevel reports a level that was never handed out.
//
// It is separate from a level that was retired, since a reader who typed a wrong
// number and a reader who typed a stale one are two different situations and telling
// them the same thing makes the handle untrustworthy.
var ErrNoLevel = errors.New("there is no such level")

// ErrLevelClosed reports a level that has ended.
var ErrLevelClosed = errors.New("that level has been closed")

// ErrRootLevel reports an attempt to close the conversation.
//
// It is named rather than folded into ErrNoLevel, since the two are different faults:
// the level is there and may not be closed, against a level that was never handed out.
var ErrRootLevel = errors.New("that level is the conversation and cannot be closed")
