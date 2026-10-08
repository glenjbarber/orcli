package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tui"
)

// levelsDispatcher is a dispatcher with an open session, cognito off, the way
// beginDispatcher builds one for /begin's own tests.
func levelsDispatcher(t *testing.T) *dispatcher {
	t.Helper()

	d := newDispatcherFor(config.Config{})
	d.withSession(tui.New(tui.Options{Model: "some/model"}))
	return d
}

func TestPaneReportsMainWithNoArgument(t *testing.T) {
	d := levelsDispatcher(t)

	out, err := d.pane("")
	if err != nil {
		t.Fatalf("pane: %v", err)
	}
	if out.Text != "the pane is main" {
		t.Errorf("pane() = %q, want it to report main", out.Text)
	}
}

func TestPaneSwitchesToEachKnownName(t *testing.T) {
	d := levelsDispatcher(t)

	for _, name := range []string{"delegate", "spawn", "main"} {
		out, err := d.pane(name)
		if err != nil {
			t.Fatalf("pane(%q): %v", name, err)
		}
		if out.Text != "the pane is "+name {
			t.Errorf("pane(%q) = %q", name, out.Text)
		}
		if got := d.session.Pane(); got != name {
			t.Errorf("session.Pane() = %q, want %q", got, name)
		}
	}
}

func TestPaneNumbersFocusMainAndBeginSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := levelsDispatcher(t)
	main := d.session
	if _, err := d.begin("worker task"); err != nil {
		t.Fatalf("begin: %v", err)
	}
	worker := d.worker
	for _, tc := range []struct {
		name  string
		want  *tui.Session
		label string
	}{{"1", worker, "1"}, {"0", main, "main"}} {
		out, err := d.pane(tc.name)
		if err != nil {
			t.Fatalf("pane(%s): %v", tc.name, err)
		}
		if d.session != tc.want || out.Text != "the pane is "+tc.label {
			t.Errorf("pane(%s) = %q, session=%p; want %p", tc.name, out.Text, d.session, tc.want)
		}
	}
}

func TestPaneRefusesAnUnknownName(t *testing.T) {
	d := levelsDispatcher(t)

	if _, err := d.pane("sidebar"); err == nil {
		t.Fatal("/pane sidebar was accepted")
	}
}

func TestSpawnOpensAWorkerLevelAndNamesIt(t *testing.T) {
	d := levelsDispatcher(t)

	out, err := d.spawn("look into the flaky test")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out.Text, "level 1") {
		t.Errorf("spawn() = %q, want it to name level 1", out.Text)
	}

	w, ok := d.session.WorkerAt(1)
	if !ok {
		t.Fatal("spawn left no worker at level 1")
	}
	if w.Question != "look into the flaky test" {
		t.Errorf("worker question = %q", w.Question)
	}
}

func TestSpawnNeedsAQuestion(t *testing.T) {
	d := levelsDispatcher(t)

	if _, err := d.spawn("   "); err == nil {
		t.Fatal("/spawn with no question was accepted")
	}
}

func TestSpawnIsRefusedUnderCognito(t *testing.T) {
	d := newDispatcherFor(config.Config{})
	d.withSession(tui.New(tui.Options{Model: "some/model", Cognito: true}))

	if _, err := d.spawn("do a thing"); !errors.Is(err, tui.ErrCognito) {
		t.Errorf("spawn under cognito = %v, want ErrCognito", err)
	}
}

func TestBtwOpensALevelAndRecordsTheQuestionThere(t *testing.T) {
	d := levelsDispatcher(t)

	out, err := d.btw("should we rename the module")
	if err != nil {
		t.Fatalf("btw: %v", err)
	}
	if !strings.Contains(out.Text, "level 1") {
		t.Errorf("btw() = %q, want it to name level 1", out.Text)
	}

	rows := d.session.RowsAt(1)
	if len(rows) == 0 || !strings.Contains(rows[0].Text, "should we rename the module") {
		t.Errorf("level 1's rows = %+v, want the question recorded there", rows)
	}
}

func TestBtwTitleIsCutToOneLine(t *testing.T) {
	d := levelsDispatcher(t)

	_, err := d.btw("first line\nsecond line that should not appear in the title")
	if err != nil {
		t.Fatalf("btw: %v", err)
	}

	level, ok := d.session.Level(1)
	if !ok {
		t.Fatal("btw opened no level")
	}
	if level.Title != "first line" {
		t.Errorf("level.Title = %q", level.Title)
	}
}

func TestBtwWorksEvenUnderCognito(t *testing.T) {
	d := newDispatcherFor(config.Config{})
	d.withSession(tui.New(tui.Options{Model: "some/model", Cognito: true}))

	if _, err := d.btw("a side question"); err != nil {
		t.Errorf("btw under cognito: %v, want it to succeed", err)
	}
}

func TestDelegateOpensALevelOutsideTheMainConversation(t *testing.T) {
	d := levelsDispatcher(t)

	out, err := d.delegate("what does this flag do")
	if err != nil {
		t.Fatalf("delegate: %v", err)
	}
	if !strings.Contains(out.Text, "level 1") {
		t.Errorf("delegate() = %q, want it to name level 1", out.Text)
	}

	rows := d.session.RowsAt(0)
	for _, row := range rows {
		if strings.Contains(row.Text, "what does this flag do") {
			t.Error("the delegated question was recorded at level 0")
		}
	}
}

func TestCloseClosesAnOpenLevel(t *testing.T) {
	d := levelsDispatcher(t)

	if _, err := d.btw("a thread"); err != nil {
		t.Fatalf("btw: %v", err)
	}

	out, err := d.close("1")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if out.Text != "closed level 1" {
		t.Errorf("close() = %q", out.Text)
	}

	level, ok := d.session.Level(1)
	if !ok || !level.Closed {
		t.Errorf("level 1 = %+v, want it closed", level)
	}
}

func TestCloseRefusesANonNumericArgument(t *testing.T) {
	d := levelsDispatcher(t)

	if _, err := d.close("abc"); err == nil {
		t.Fatal("/close abc was accepted")
	}
}

func TestCloseRefusesTheRootLevel(t *testing.T) {
	d := levelsDispatcher(t)

	if _, err := d.close("0"); !errors.Is(err, tui.ErrRootLevel) {
		t.Errorf("close(0) = %v, want ErrRootLevel", err)
	}
}

func TestCloseRefusesAnUnknownLevel(t *testing.T) {
	d := levelsDispatcher(t)

	if _, err := d.close("99"); !errors.Is(err, tui.ErrNoLevel) {
		t.Errorf("close(99) = %v, want ErrNoLevel", err)
	}
}

func TestCopyWithNoArgumentCopiesTheWholeConversation(t *testing.T) {
	d := levelsDispatcher(t)
	d.session.Notice("something the reader said", 0, tui.RoleNotice)

	out, err := d.copyCmd("")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !strings.Contains(out.Text, "something the reader said") {
		t.Errorf("copy() = %q, want the conversation's own rows", out.Text)
	}
}

func TestCopyNCopiesThatLevel(t *testing.T) {
	d := levelsDispatcher(t)
	if _, err := d.btw("a branched question"); err != nil {
		t.Fatalf("btw: %v", err)
	}

	out, err := d.copyCmd("1")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !strings.Contains(out.Text, "a branched question") {
		t.Errorf("copy(1) = %q", out.Text)
	}
}

func TestCopyOfAClosedLevelStillReturnsItsText(t *testing.T) {
	d := levelsDispatcher(t)
	if _, err := d.btw("a branched question"); err != nil {
		t.Fatalf("btw: %v", err)
	}
	if _, err := d.close("1"); err != nil {
		t.Fatalf("close: %v", err)
	}

	out, err := d.copyCmd("1")
	if err != nil {
		t.Fatalf("copy of a closed level returned an error: %v", err)
	}
	if !strings.Contains(out.Text, "closed") || !strings.Contains(out.Text, "a branched question") {
		t.Errorf("copy(1) after close = %q", out.Text)
	}
}

func TestCopyRefusesANonNumericArgument(t *testing.T) {
	d := levelsDispatcher(t)

	if _, err := d.copyCmd("abc"); err == nil {
		t.Fatal("/copy abc was accepted")
	}
}

func TestQueueAddsAFollowUpPrompt(t *testing.T) {
	d := levelsDispatcher(t)

	out, err := d.queue("run the tests again after")
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if !strings.Contains(out.Text, "run the tests again after") {
		t.Errorf("queue() = %q", out.Text)
	}
	if d.session.QueueLen() != 1 {
		t.Errorf("QueueLen() = %d, want 1", d.session.QueueLen())
	}

	next, ok := d.session.Drain()
	if !ok || next != "run the tests again after" {
		t.Errorf("Drain() = %q, %v", next, ok)
	}
}

func TestQueueNeedsText(t *testing.T) {
	d := levelsDispatcher(t)

	if _, err := d.queue("   "); err == nil {
		t.Fatal("/queue with no text was accepted")
	}
}

func TestRedirectStopsTheTurnInFlight(t *testing.T) {
	d := levelsDispatcher(t)

	called := false
	d.session.SetCancel(func() { called = true })

	out, err := d.redirect("")
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	if !called {
		t.Error("redirect did not call the cancel")
	}
	if !strings.Contains(out.Text, "stopped") {
		t.Errorf("redirect() = %q", out.Text)
	}

	state, _ := d.session.State()
	if state != tui.StateIdle {
		t.Errorf("state after redirect = %q, want idle", state)
	}
}

func TestRedirectRefusesWhenNothingIsRunning(t *testing.T) {
	d := levelsDispatcher(t)

	if _, err := d.redirect(""); err == nil {
		t.Fatal("/redirect with no turn in flight was accepted")
	}
}
