package tui

import (
	"errors"
	"sync"
	"testing"
)

// TestLevelZeroAlwaysExists covers the root. A reader never meets a session where the
// conversation they are in has no number, so `/copy 0` has something to copy from the
// first moment.
func TestLevelZeroAlwaysExists(t *testing.T) {
	s := New(Options{Model: "some/model"})

	level, ok := s.Level(0)
	if !ok {
		t.Fatal("level 0 is not there, want it always present")
	}
	if level.Closed {
		t.Error("level 0 is closed, want it open")
	}
	if level.Parent != rootParent {
		t.Errorf("level 0 has parent %d, want %d: a root has no parent", level.Parent, rootParent)
	}
}

// TestOpenLevelTakesItsOwnNumber covers the decision a /btw rests on. A thread does
// not share its parent's number, because a thread writes a findings file that outlives
// the session and a reader opening it later has no gutter to read.
func TestOpenLevelTakesItsOwnNumber(t *testing.T) {
	s := New(Options{Model: "some/model"})

	thread, err := s.OpenLevel(0, "what does the log do?")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}

	if thread.Number == 0 {
		t.Error("the thread took level 0, want a number of its own")
	}
	if thread.Parent != 0 {
		t.Errorf("the thread has parent %d, want 0", thread.Parent)
	}
	if thread.Title != "what does the log do?" {
		t.Errorf("the thread is titled %q, want the question it was asked", thread.Title)
	}
}

// TestOpenLevelNumbersDoNotReuse covers the property that makes a handle trustworthy.
// A reader who copied level 3 an hour ago and types `/copy 3` now must not get
// whatever took the number.
func TestOpenLevelNumbersDoNotReuse(t *testing.T) {
	s := New(Options{Model: "some/model"})

	first, err := s.OpenLevel(0, "the first thread")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}
	if err := s.CloseLevel(first.Number); err != nil {
		t.Fatalf("CloseLevel: %v", err)
	}

	second, err := s.OpenLevel(0, "the second thread")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}

	if second.Number == first.Number {
		t.Errorf("a retired level %d was handed out again", first.Number)
	}
}

// TestALevelIsSpentEvenIfTheThreadIsAbandoned covers the hole the reader sees. The
// number is allocated when the thread is opened rather than when it is asked, so a
// reader who walks away from a thread leaves a gap rather than a number that could be
// handed out twice.
func TestALevelIsSpentEvenIfTheThreadIsAbandoned(t *testing.T) {
	s := New(Options{Model: "some/model"})

	abandoned, err := s.OpenLevel(0, "a thread nobody asked")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}

	// The thread is never asked anything and stays open.
	level, ok := s.Level(abandoned.Number)
	if !ok {
		t.Fatal("the level vanished, want it held as open")
	}
	if level.Closed {
		t.Error("the level is closed, want it open: nothing ended it")
	}

	// The next thread takes a number after it, not the same one.
	next, err := s.OpenLevel(0, "the next thread")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}
	if next.Number <= abandoned.Number {
		t.Errorf("the next thread took %d, want above the abandoned %d",
			next.Number, abandoned.Number)
	}
}

// TestCloseLevelRetiresRatherThanRemoves covers the tombstone. A reader who asks about
// a retired level is told what it was, which is how a reader distinguishes a stale
// handle from a mistyped number.
func TestCloseLevelRetiresRatherThanRemoves(t *testing.T) {
	s := New(Options{Model: "some/model"})

	thread, err := s.OpenLevel(0, "a side question")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}
	if err := s.CloseLevel(thread.Number); err != nil {
		t.Fatalf("CloseLevel: %v", err)
	}

	level, ok := s.Level(thread.Number)
	if !ok {
		t.Fatal("a closed level was removed, want it kept as a tombstone")
	}
	if !level.Closed {
		t.Error("the level is not marked closed after CloseLevel")
	}
	if level.Title != "a side question" {
		t.Errorf("the tombstone is titled %q, want the question it was", level.Title)
	}
}

// TestClosingTwiceIsNotAnError covers the ordinary sequence. A thread ends itself when
// it has finished and a reader closes it afterwards, and refusing the second would
// make the reader's action the one that fails.
func TestClosingTwiceIsNotAnError(t *testing.T) {
	s := New(Options{Model: "some/model"})

	thread, err := s.OpenLevel(0, "a thread")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}

	if err := s.CloseLevel(thread.Number); err != nil {
		t.Fatalf("the first close: %v", err)
	}
	if err := s.CloseLevel(thread.Number); err != nil {
		t.Errorf("the second close returned %v, want nil", err)
	}
}

// TestClosingTheRootIsRefused covers the one level a reader cannot close. Level 0 is
// the conversation they are in, and a session whose own conversation is closed is a
// session with nowhere to type.
func TestClosingTheRootIsRefused(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if err := s.CloseLevel(0); err == nil {
		t.Error("the root level was closed, want a refusal")
	}

	level, _ := s.Level(0)
	if level.Closed {
		t.Error("level 0 is closed after a refused close")
	}
}

// TestClosingAnAbsentLevelIsRefused covers the difference the reader sees. A level that
// was never handed out is not the same as one that was retired, and saying so is what
// makes the handle trustworthy.
func TestClosingAnAbsentLevelIsRefused(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if err := s.CloseLevel(9); !errors.Is(err, ErrNoLevel) {
		t.Errorf("closing level 9 returned %v, want ErrNoLevel", err)
	}
}

// TestCopyLevelSeparatesAbsentFromClosed covers the distinction the whole tombstone is
// for. A reader who typed a wrong number and a reader who typed a stale one get
// different answers.
func TestCopyLevelSeparatesAbsentFromClosed(t *testing.T) {
	s := New(Options{Model: "some/model"})

	thread, err := s.OpenLevel(0, "a side question")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}
	s.Log().Append(Row{Text: "an answer", Level: thread.Number, Kind: KindReply})
	if err := s.CloseLevel(thread.Number); err != nil {
		t.Fatalf("CloseLevel: %v", err)
	}

	// A closed level still answers and says so.
	rows, err := s.CopyLevel(thread.Number)
	if !errors.Is(err, ErrLevelClosed) {
		t.Errorf("copying a closed level returned %v, want ErrLevelClosed", err)
	}
	if len(rows) != 1 {
		t.Errorf("a closed level gave %d rows, want its rows back anyway", len(rows))
	}

	// An absent level says something different.
	if _, err := s.CopyLevel(9); !errors.Is(err, ErrNoLevel) {
		t.Errorf("copying level 9 returned %v, want ErrNoLevel", err)
	}
}

// TestCopyLevelOfAnOpenThreadIsNotAnError covers the ordinary case. A live thread is
// what a reader copies, so nothing about it is a fault.
func TestCopyLevelOfAnOpenThreadIsNotAnError(t *testing.T) {
	s := New(Options{Model: "some/model"})

	thread, err := s.OpenLevel(0, "a side question")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}
	s.Log().Append(Row{Text: "an answer", Level: thread.Number, Kind: KindReply})

	rows, err := s.CopyLevel(thread.Number)
	if err != nil {
		t.Errorf("copying an open level returned %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Errorf("copying an open level gave %d rows, want 1", len(rows))
	}
}

// TestLevelsIncludeRetiredOnes covers the bar. A list that silently dropped closed
// entries is how a reader concludes they mistyped.
func TestLevelsIncludeRetiredOnes(t *testing.T) {
	s := New(Options{Model: "some/model"})

	first, _ := s.OpenLevel(0, "the first thread")
	second, _ := s.OpenLevel(0, "the second thread")
	if err := s.CloseLevel(first.Number); err != nil {
		t.Fatalf("CloseLevel: %v", err)
	}

	all := s.Levels()
	if len(all) != 3 {
		t.Fatalf("Levels has %d entries, want 3: the root and both threads", len(all))
	}

	var closed, open int
	for _, level := range all {
		if level.Closed {
			closed++
		} else {
			open++
		}
	}
	if closed != 1 {
		t.Errorf("%d levels are closed, want 1", closed)
	}
	if open != 2 {
		t.Errorf("%d levels are open, want 2", open)
	}

	// The ordered view is ordered.
	for i := 1; i < len(all); i++ {
		if all[i-1].Number >= all[i].Number {
			t.Fatalf("Levels is not ordered: %v", all)
		}
	}

	// The second thread is untouched by the first closing.
	if level, ok := s.Level(second.Number); !ok || level.Closed {
		t.Errorf("closing one thread closed another: %v", second.Number)
	}
}

// TestOpenLevelsExcludesRetired covers the list a new thread branches from. A reader
// asking what a thread can branch from wants no closed entries in it.
func TestOpenLevelsExcludesRetired(t *testing.T) {
	s := New(Options{Model: "some/model"})

	thread, _ := s.OpenLevel(0, "a thread")
	if err := s.CloseLevel(thread.Number); err != nil {
		t.Fatalf("CloseLevel: %v", err)
	}
	if _, err := s.OpenLevel(0, "another thread"); err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}

	open := s.OpenLevels()
	for _, level := range open {
		if level.Closed {
			t.Errorf("a closed level %d is offered as open", level.Number)
		}
	}
	if len(open) != 2 {
		t.Errorf("OpenLevels has %d entries, want the root and the live thread", len(open))
	}
}

// TestOpenLevelFromAnAbsentParentIsRefused covers the check that a thread branches from
// something that exists. A parent that was never handed out is a level a reader cannot
// have meant.
func TestOpenLevelFromAnAbsentParentIsRefused(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if _, err := s.OpenLevel(9, "a thread"); !errors.Is(err, ErrNoLevel) {
		t.Errorf("opening from level 9 returned %v, want ErrNoLevel", err)
	}
}

// TestAThreadCanBranchFromAThread covers that a parent need not be the root. A thread
// branching from a live thread is an ordinary sequence, and the parent is recorded
// either way.
func TestAThreadCanBranchFromAThread(t *testing.T) {
	s := New(Options{Model: "some/model"})

	first, err := s.OpenLevel(0, "the first thread")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}

	second, err := s.OpenLevel(first.Number, "a thread of the thread")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}

	if second.Parent != first.Number {
		t.Errorf("the second thread has parent %d, want %d", second.Parent, first.Number)
	}
	if first.Parent != 0 {
		t.Errorf("the first thread has parent %d, want 0", first.Parent)
	}
}

// TestConcurrentOpenAndClose covers the levels table under load, since a thread ending
// itself races a reader opening another.
func TestConcurrentOpenAndClose(t *testing.T) {
	s := New(Options{Model: "some/model"})

	var wg sync.WaitGroup
	var opened []int
	var mu sync.Mutex

	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			level, err := s.OpenLevel(0, "a thread")
			if err != nil {
				t.Errorf("OpenLevel: %v", err)
				return
			}
			mu.Lock()
			opened = append(opened, level.Number)
			mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			s.Levels()
		}
	}()
	wg.Wait()

	// Close everything that was opened, then check none of the numbers collided.
	for _, n := range opened {
		if err := s.CloseLevel(n); err != nil {
			t.Errorf("CloseLevel(%d): %v", n, err)
		}
	}

	seen := map[int]bool{}
	for _, n := range opened {
		if seen[n] {
			t.Errorf("level %d was handed out twice", n)
		}
		seen[n] = true
	}
	if len(seen) != len(opened) {
		t.Errorf("%d numbers were handed out and %d are distinct", len(opened), len(seen))
	}
}
