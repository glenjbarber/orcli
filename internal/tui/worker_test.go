package tui

import (
	"errors"
	"strings"
	"testing"
)

// TestSpawnTakesALevelOfItsOwn is the whole reason this file exists. A worker
// without a level is background work the reader can watch but cannot copy out of,
// and a worker whose findings have to be selected by hand is a worker whose
// findings do not get used.
func TestSpawnTakesALevelOfItsOwn(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "please work on feature X")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if got, want := w.Level.Number, 1; got != want {
		t.Errorf("the worker took level %d, want %d", got, want)
	}
	if got, want := w.Level.Parent, 0; got != want {
		t.Errorf("the worker branches from %d, want %d", got, want)
	}
}

// TestSpawnEchoesItsQuestionIntoTheLog covers the row a reader needs above a
// background answer. A log that opened onto a worker's findings with no question
// gives no way to tell what they are findings of.
func TestSpawnEchoesItsQuestionIntoTheLog(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "please work on feature X")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	rows := s.RowsAt(w.Level.Number)
	if len(rows) != 1 {
		t.Fatalf("level %d holds %d rows, want 1", w.Level.Number, len(rows))
	}
	if rows[0].Kind != KindQuestion {
		t.Errorf("the row is kind %d, want a question", rows[0].Kind)
	}
	if rows[0].Text != "please work on feature X" {
		t.Errorf("the row is %q, want the question as typed", rows[0].Text)
	}
}

// TestSpawnBranchesFromTheLevelItIsGiven rather than from the root, since a reader
// deep in a side question wants the worker to inherit that prefix.
func TestSpawnBranchesFromTheLevelItIsGiven(t *testing.T) {
	s := New(Options{Model: "some/model"})

	btw, err := s.OpenLevel(0, "a side question")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}

	w, err := s.Spawn(btw.Number, "work from here")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if got, want := w.Level.Parent, btw.Number; got != want {
		t.Errorf("the worker branches from %d, want %d", got, want)
	}
}

// TestSpawnRefusesInCognito covers the refusal and the reason for it. A worker runs
// programs and writes files, and a file it wrote is a record of what the model did
// even though the transcript records nothing.
func TestSpawnRefusesInCognito(t *testing.T) {
	s := New(Options{Model: "some/model", Cognito: true})

	if _, err := s.Spawn(0, "please work on feature X"); !errors.Is(err, ErrCognito) {
		t.Errorf("Spawn returned %v, want ErrCognito", err)
	}
}

// TestARefusedSpawnDoesNotSpendALevel is the detail that makes the refusal
// recoverable. A reader who turns cognito off and tries again should get the number
// they would have had, not one further along with a hole before it.
func TestARefusedSpawnDoesNotSpendALevel(t *testing.T) {
	s := New(Options{Model: "some/model", Cognito: true})

	if _, err := s.Spawn(0, "a question"); err == nil {
		t.Fatal("a worker started in cognito, want a refusal")
	}

	// Cognito is a session option and the field is read each time, so a reader
	// turning it off is a reader turning off the field on a session they hold.
	s.opts.Cognito = false

	w, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn after cognito: %v", err)
	}
	if got, want := w.Level.Number, 1; got != want {
		t.Errorf("the worker took level %d, want %d: a refused spawn spent a number",
			got, want)
	}
}

// TestSpawnRefusesNoQuestion covers the case where a worker would run for no reason,
// and the reason it was started is the one thing the reader typed.
func TestSpawnRefusesNoQuestion(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if _, err := s.Spawn(0, ""); !errors.Is(err, ErrNoQuestion) {
		t.Errorf("Spawn returned %v, want ErrNoQuestion", err)
	}
}

// TestSpawnRefusesNoModel covers the check every caller shares.
func TestSpawnRefusesNoModel(t *testing.T) {
	s := New(Options{})

	if _, err := s.Spawn(0, "a question"); !errors.Is(err, ErrNoModel) {
		t.Errorf("Spawn returned %v, want ErrNoModel", err)
	}
}

// TestSpawnRefusesAnAbsentParent covers the case where the level the reader named is
// not one, since a worker branching from a number that was never handed out has no
// conversation to inherit.
func TestSpawnRefusesAnAbsentParent(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if _, err := s.Spawn(9, "a question"); !errors.Is(err, ErrNoLevel) {
		t.Errorf("Spawn returned %v, want ErrNoLevel", err)
	}
}

// TestCopiedWorkerIsItsQuestionAndAnswer covers the shape of what `/copy N` writes.
// A worker copied out of the session is a question and the answer to it, and a
// reader pasting that into a ticket wants both rather than an answer with no
// question attached.
func TestCopiedWorkerIsItsQuestionAndAnswer(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "please work on feature X")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	s.Deliver("the findings", w.Level.Number)
	s.Finish(w, WorkerDone, "done")

	got, level, err := s.CopyText(w.Level.Number)
	if err != nil && !errors.Is(err, ErrLevelClosed) {
		t.Fatalf("CopyText: %v", err)
	}
	if !errors.Is(err, ErrLevelClosed) {
		t.Error("a finished worker copied without saying its level is closed")
	}

	want := "please work on feature X\nthe findings\ndone\n"
	if got != want {
		t.Errorf("CopyText gave\n%q\nwant\n%q", got, want)
	}
	if level.Number != w.Level.Number {
		t.Errorf("CopyText named level %d, want %d", level.Number, w.Level.Number)
	}
}

// TestACopyOfAWorkerDoesNotReachAnotherWorker covers the point of the level. Two
// workers running at once must not share a copy, since that is the failure a pane
// would have had.
func TestACopyOfAWorkerDoesNotReachAnotherWorker(t *testing.T) {
	s := New(Options{Model: "some/model"})

	first, err := s.Spawn(0, "the first question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	second, err := s.Spawn(0, "the second question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	s.Deliver("the first answer", first.Level.Number)
	s.Deliver("the second answer", second.Level.Number)

	got, _, err := s.CopyText(first.Level.Number)
	if err != nil {
		t.Fatalf("CopyText: %v", err)
	}
	if strings.Contains(got, "second") {
		t.Errorf("copying level %d reached another worker:\n%s", first.Level.Number, got)
	}
	if !strings.Contains(got, "the first answer") {
		t.Errorf("the copy does not carry its own answer:\n%s", got)
	}
}

// TestCopyTextOfAnAbsentLevelIsNothing covers the reader who mistyped. A number never
// handed out gets nothing at all, which is a different answer from a closed one.
func TestCopyTextOfAnAbsentLevelIsNothing(t *testing.T) {
	s := New(Options{Model: "some/model"})

	got, level, err := s.CopyText(9)
	if !errors.Is(err, ErrNoLevel) {
		t.Errorf("CopyText returned %v, want ErrNoLevel", err)
	}
	if got != "" {
		t.Errorf("CopyText gave %q for a level that does not exist", got)
	}
	if level.Number != 0 {
		t.Errorf("CopyText named level %d for one that does not exist", level.Number)
	}
}

// TestACopyStripsWhatTheTerminalWouldActOn covers the plain-text contract reaching
// the clipboard. A selection out of a terminal already carries no escapes, so a copy
// that did carry one would paste a cursor movement into whatever it went into.
func TestACopyStripsWhatTheTerminalWouldActOn(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	s.Deliver("before\x1b[2Jafter", w.Level.Number)

	got, _, err := s.CopyText(w.Level.Number)
	if err != nil {
		t.Fatalf("CopyText: %v", err)
	}
	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("the copy carries an escape: %q", got)
	}
	if !strings.Contains(got, "beforeafter") {
		t.Errorf("the copy is %q, want the escape gone", got)
	}
}

// TestTheCredentialIsNotInACopiedWorker covers the rule that makes the plain-text
// contract worth having. The clipboard is where a credential least wants to be.
func TestTheCredentialIsNotInACopiedWorker(t *testing.T) {
	const key = "sk-or-v1-the-credential"
	s := New(Options{APIKey: key, Model: "some/model"})

	w, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	s.Deliver("an answer", w.Level.Number)
	s.Finish(w, WorkerDone, "done")

	got, _, err := s.CopyText(w.Level.Number)
	if err != nil && !errors.Is(err, ErrLevelClosed) {
		t.Fatalf("CopyText: %v", err)
	}
	if strings.Contains(got, key) {
		t.Errorf("the copied text carries the credential: %q", got)
	}
}

// TestFinishRetiresTheWorkersLevel covers the rule that a finished worker is a
// closed exchange. It is why `/close` and a worker finishing are the same operation.
func TestFinishRetiresTheWorkersLevel(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	s.Finish(w, WorkerDone, "done")

	level, known := s.Level(w.Level.Number)
	if !known {
		t.Fatal("the level is gone, want a tombstone a reader can be told about")
	}
	if !level.Closed {
		t.Error("the level is open after the worker finished")
	}
	if got := s.WorkerStateOf(w); got != WorkerDone {
		t.Errorf("the worker state is %q, want done", got)
	}
}

// TestFinishTwiceIsNotAFault covers the reader who asks twice. The second ask is not
// a failure the reader caused, and refusing it would make the second press the one
// that fails.
func TestFinishTwiceIsNotAFault(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	s.Finish(w, WorkerDone, "done")
	before := s.Log().Len()
	s.Finish(w, WorkerDone, "done")

	if got := s.Log().Len(); got != before {
		t.Errorf("a second Finish wrote a row: %d became %d", before, got)
	}
	if got := s.WorkerStateOf(w); got != WorkerDone {
		t.Errorf("a second Finish changed the state to %q", got)
	}
}

// TestStopEndsTheWorkersRequest covers the reader who presses the key, since a
// worker that cannot be stopped is one that keeps spending the allowance after they
// have walked away.
func TestStopEndsTheWorkersRequest(t *testing.T) {
	s := New(Options{Model: "some/model"})

	ended := false
	w, err := s.SpawnWithCancel(0, "a long question", func() { ended = true })
	if err != nil {
		t.Fatalf("SpawnWithCancel: %v", err)
	}

	s.Stop(w, "stopped by the reader")

	if !ended {
		t.Error("Stop did not close the request")
	}
	if got := s.WorkerStateOf(w); got != WorkerStopped {
		t.Errorf("the worker state is %q, want stopped", got)
	}
	if level, _ := s.Level(w.Level.Number); !level.Closed {
		t.Error("a stopped worker's level is still open")
	}
}

// TestWorkerAtFindsTheWorkerAndRefusesTheRest covers the difference between a level
// and a worker. A `/btw` has a level and is answered in the session, so asking for
// the worker of one finds none.
func TestWorkerAtFindsTheWorkerAndRefusesTheRest(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	btw, err := s.OpenLevel(0, "a side question")
	if err != nil {
		t.Fatalf("OpenLevel: %v", err)
	}

	if got, ok := s.WorkerAt(w.Level.Number); !ok || got != w {
		t.Error("WorkerAt did not find the worker")
	}
	if _, ok := s.WorkerAt(btw.Number); ok {
		t.Error("WorkerAt found a worker for a level that is not one")
	}
}

// TestAWorkersStateIsNotTheSessionsState covers the two being separate. A worker
// running behind the reader does not change what the reader is in, and folding them
// would mean background work making the interface look busy.
func TestAWorkersStateIsNotTheSessionsState(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if state, _ := s.State(); state != StateIdle {
		t.Errorf("the session is %q after a spawn, want idle: a worker is not the reader's turn",
			state)
	}
	if got := s.WorkerStateOf(w); got != WorkerRunning {
		t.Errorf("the worker is %q, want running", got)
	}
}

// TestSpawnRefusesTheRootLevelClosing covers the one level that cannot be closed,
// since a session whose own conversation is closed is a session with nowhere to
// type.
func TestSpawnRefusesTheRootLevelClosing(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if err := s.CloseLevel(0); !errors.Is(err, ErrRootLevel) {
		t.Errorf("CloseLevel(0) returned %v, want ErrRootLevel", err)
	}
}

// TestPaneStateIsEmptyBeforeAnyWorker covers the one state the pane bar colours
// neither colour for: a session that has never spawned a worker has nothing
// running and nothing finished to report.
func TestPaneStateIsEmptyBeforeAnyWorker(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if got := s.PaneState(); got != "" {
		t.Errorf("PaneState on a fresh session is %q, want empty", got)
	}
}

// TestPaneStateIsRunningWhileAWorkerIs covers the colour a reader watches for:
// the pane bar reports "running" for as long as a worker is in flight there.
func TestPaneStateIsRunningWhileAWorkerIs(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if _, err := s.Spawn(0, "a question"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if got := s.PaneState(); got != "running" {
		t.Errorf("PaneState with a worker in flight is %q, want running", got)
	}
}

// TestPaneStateIsDoneAfterAWorkerFinishes covers the sticky indicator: once a
// worker finishes on its own and none is running, the pane stays "done" rather
// than reverting to empty, since a reader who has not looked back since should
// still find the pane coloured the way the worker left it.
func TestPaneStateIsDoneAfterAWorkerFinishes(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	s.Finish(w, WorkerDone, "finished")

	if got := s.PaneState(); got != "done" {
		t.Errorf("PaneState after a worker finishes is %q, want done", got)
	}
}

// TestPaneStateIsNotDoneAfterAWorkerStops covers the one state Finish can record
// that is not "done": a worker the reader stopped, or one that failed, is not the
// signal a reader asked the done colour for.
func TestPaneStateIsNotDoneAfterAWorkerStops(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	s.Stop(w, "stopped")

	if got := s.PaneState(); got != "" {
		t.Errorf("PaneState after a worker is stopped is %q, want empty", got)
	}
}

// TestPaneStateReturnsToRunningAfterAnotherSpawn covers the sticky "done" clearing
// the way it was designed to: a new worker starting is what moves the pane back to
// "running", not a timer.
func TestPaneStateReturnsToRunningAfterAnotherSpawn(t *testing.T) {
	s := New(Options{Model: "some/model"})

	first, err := s.Spawn(0, "a question")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	s.Finish(first, WorkerDone, "finished")

	if _, err := s.Spawn(0, "a second question"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if got := s.PaneState(); got != "running" {
		t.Errorf("PaneState with a second worker running is %q, want running", got)
	}
}
