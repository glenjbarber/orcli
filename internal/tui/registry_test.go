package tui

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestTheGroupCountsWhatIsRunning covers the ordinary case. A turn raises the count
// before its goroutine starts and lowers it after its answer is drawn, so a wait that
// returns means the work finished rather than merely being cancelled.
func TestTheGroupCountsWhatIsRunning(t *testing.T) {
	g := newGroup()

	if err := g.Add(); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := g.count; got != 1 {
		t.Errorf("the count is %d, want 1", got)
	}

	g.Done()
	if got := g.count; got != 0 {
		t.Errorf("the count is %d after Done, want 0", got)
	}
}

// TestCloseWaitsForWhatIsCounted is the property the group exists for. Returning to the
// shell with a request still in flight is a goroutine writing to a terminal that has
// been handed back.
func TestCloseWaitsForWhatIsCounted(t *testing.T) {
	g := newGroup()
	if err := g.Add(); err != nil {
		t.Fatalf("Add: %v", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- g.Close() }()

	// The wait has begun before the count is lowered, or the test is not testing the
	// wait at all. Waiting rather than sleeping is what keeps this from passing
	// because the machine was slow.
	select {
	case err := <-waited:
		t.Fatalf("Close returned while a turn was still counted: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	g.Done()

	select {
	case err := <-waited:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Close did not return after the count reached zero")
	}
}

// TestTheGroupIsRefusedOnceCloseHasBegun is the case that makes the wait safe. A turn
// raised after Close began waiting is a counter nobody is waiting for, and a reader
// handed a terminal with that turn running in it.
func TestTheGroupIsRefusedOnceCloseHasBegun(t *testing.T) {
	g := newGroup()

	if err := g.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := g.Add()
	if err == nil {
		t.Fatal("a turn was accepted after Close had begun")
	}
	if !errors.Is(err, ErrClosing) {
		t.Errorf("the refusal is %v, want it to carry ErrClosing", err)
	}
}

// TestCloseIsSafeToCallTwice covers a caller that reaches it from two paths out, which
// is what a defer and an explicit call amount to.
func TestCloseIsSafeToCallTwice(t *testing.T) {
	g := newGroup()

	if err := g.Close(); err != nil {
		t.Fatalf("the first Close: %v", err)
	}
	if err := g.Close(); err != nil {
		t.Errorf("the second Close: %v", err)
	}
}

// TestCloseFromTwoGoroutinesWaitsForBoth covers the case two paths out reach it at once,
// which is what a defer and a signal handler amount to. One of them waits on the
// condition while the other returns at once, and both have to come back.
func TestCloseFromTwoGoroutinesWaitsForBoth(t *testing.T) {
	g := newGroup()
	if err := g.Add(); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			if err := g.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	g.Done()

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Error("one of two Closes did not return")
	}
}

// TestDoneOnAnEmptyGroupDoesNotGoNegative covers the boundary. A count below zero is a
// wait that returns before the work is done, which is the failure this exists to stop,
// arriving from the other direction.
func TestDoneOnAnEmptyGroupDoesNotGoNegative(t *testing.T) {
	g := newGroup()

	g.Done()
	g.Done()

	if got := g.count; got != 0 {
		t.Errorf("the count is %d after two Dones on an empty group, want 0", got)
	}
}

// TestTheGroupIsSafeUnderTheDetector covers the property a count has to have, since the
// loop raises it from the key goroutine and lowers it from the turn's.
//
// Every Add that is accepted is matched by a Done, which is the discipline the caller
// owes: a count raised and never lowered is a wait that returns never, and that is the
// caller's bug rather than the counter's.
func TestTheGroupIsSafeUnderTheDetector(t *testing.T) {
	g := newGroup()

	var mu sync.Mutex
	raised := 0

	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := g.Add(); err != nil {
				return
			}

			mu.Lock()
			raised++
			mu.Unlock()

			g.Done()
		}()
	}
	wg.Wait()

	if err := g.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if raised == 0 {
		t.Error("no Add was accepted, so the test exercised nothing")
	}
}

// TestCloseOnAGroupWithNothingIsImmediate covers the ordinary end of a session, where
// nothing was ever counted and leaving should not wait.
func TestCloseOnAGroupWithNothingIsImmediate(t *testing.T) {
	g := newGroup()

	done := make(chan error, 1)
	go func() { done <- g.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Close waited on a group that had nothing counted")
	}
}

// TestErrClosingIsDistinctFromTheWorkFailing covers why the refusal is named. A turn
// refused for this reason never started, and a turn refused for any other reason did,
// so a caller reporting the reason is reporting something true.
func TestErrClosingIsDistinctFromTheWorkFailing(t *testing.T) {
	if errors.Is(ErrClosing, ErrNoModel) {
		t.Error("leaving and having no model are reported as the same thing")
	}
	if ErrClosing.Error() == "" {
		t.Error("the refusal has no message")
	}
}
