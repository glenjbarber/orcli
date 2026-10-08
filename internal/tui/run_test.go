package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// errFailedTurn is a stand-in failure for TestStartDoesNotDrainTheQueueAfterAFailedTurn.
var errFailedTurn = errors.New("the turn failed")

func TestCtrlBPaneNavigationConsumesNavigationKeys(t *testing.T) {
	main := New(Options{})
	worker := New(Options{})
	l := &interfaceLoop{
		session: main,
		navigatePane: func(direction int) *Session {
			if direction > 0 {
				return worker
			}
			return main
		},
	}

	l.act(context.Background(), KeyCtrlB, 0)
	if !l.prefixPending {
		t.Fatal("Ctrl+B did not enter prefix mode")
	}
	l.act(context.Background(), KeyRune, 'N')
	if l.session != worker {
		t.Fatal("Ctrl+B N did not focus pane 1")
	}
	if got := worker.Editor().Text(); got != "" {
		t.Fatalf("navigation key was inserted into pane 1 input: %q", got)
	}

	l.act(context.Background(), KeyCtrlB, 0)
	l.act(context.Background(), KeyRune, 'p')
	if l.session != main {
		t.Fatal("Ctrl+B p did not return focus to pane 0")
	}
	if got := main.Editor().Text(); got != "" {
		t.Fatalf("navigation key was inserted into pane 0 input: %q", got)
	}

	l.act(context.Background(), KeyRune, 'n')
	if got := main.Editor().Text(); got != "n" {
		t.Fatalf("unprefixed n = %q, want literal input", got)
	}
}

func TestCtrlBDoublePressInsertsLiteralControlB(t *testing.T) {
	s := New(Options{})
	l := &interfaceLoop{session: s}
	l.act(context.Background(), KeyCtrlB, 0)
	l.act(context.Background(), KeyCtrlB, 0)
	if got := s.Editor().Text(); got != "\x02" {
		t.Fatalf("double Ctrl+B inserted %q, want one literal control-B byte", got)
	}
}

// TestStartSendsHELOAutomatically covers the startup HELO at the level Start's
// own event loop runs it at, without opening a real terminal screen: a test binary
// has none, and driving tview's own Application through a fake screen is more than
// this hook needs proving. interfaceLoop.start is the exact call Start makes for a
// non-empty HELO, on the same group a reader's own line would be started on, so
// calling it directly here exercises the real mechanism - the goroutine, the
// group accounting, and the session writes - with no key pressed and no line
// runner consulted at all.
func TestStartSendsHELOAutomatically(t *testing.T) {
	s := New(Options{Model: "some/model"})

	var asked string
	var gotSilent bool
	ask := func(_ context.Context, question string, level int, silent bool) error {
		asked = question
		gotSilent = silent
		s.Deliver("HELO back", level)
		return nil
	}

	l := &interfaceLoop{session: s, ask: ask, group: newGroup()}
	maybeSendHELO(context.Background(), l, "introduce yourself")
	if err := l.group.Close(); err != nil {
		t.Fatalf("group.Close: %v", err)
	}

	if asked != "introduce yourself" {
		t.Errorf("ask was called with %q, want the HELO text", asked)
	}
	if !gotSilent {
		t.Error("ask was called with silent = false, want the HELO sent silent so its own text never becomes a log row")
	}

	rows := s.Log().Rows()
	if got := rows[len(rows)-1].Text; got != "HELO back" {
		t.Errorf("last log row = %q, want the HELO's own reply, written with no line submitted", got)
	}
	for _, row := range rows {
		if row.Text == "introduce yourself" {
			t.Errorf("the HELO's own question text appeared as a log row: %+v, want only its reply visible", row)
		}
	}
}

// TestStartSendsNoHELOWhenEmpty covers the other side: an empty HELO is a
// caller's choice not to greet, and maybeSendHELO - the exact call Start makes -
// starts no turn at all when it is given one.
func TestStartSendsNoHELOWhenEmpty(t *testing.T) {
	s := New(Options{Model: "some/model"})

	called := false
	ask := func(context.Context, string, int, bool) error {
		called = true
		return nil
	}

	l := &interfaceLoop{session: s, ask: ask, group: newGroup()}
	maybeSendHELO(context.Background(), l, "")
	_ = l.group.Close()

	if called {
		t.Error("ask was called despite an empty HELO")
	}
}

// TestMaybeSendHELOSkipsWithNoAsk covers a loop built with no ask at all - a
// session with no model configured, say - sending nothing rather than calling a
// nil function.
func TestMaybeSendHELOSkipsWithNoAsk(t *testing.T) {
	s := New(Options{Model: "some/model"})
	l := &interfaceLoop{session: s, group: newGroup()}

	maybeSendHELO(context.Background(), l, "introduce yourself")
	_ = l.group.Close()
}

// TestStartDrainsAQueuedPromptOnceTheTurnFinishesClean covers the chain /queue is
// built on: l.start's own goroutine, after a turn returns with no error, drains the
// session's queue and starts whatever it finds next on the same group, the way the
// hello above is started - with no key pressed, and no second call this test has to
// make itself.
func TestStartDrainsAQueuedPromptOnceTheTurnFinishesClean(t *testing.T) {
	s := New(Options{Model: "some/model"})
	s.Enqueue("the queued follow-up")

	// done is signalled once the second, chained turn has run, so the test waits
	// for the chain to actually happen rather than for group.Close - which would
	// itself mark the group closing and refuse the chained start's own Add before
	// it ever runs, racing ahead of the very thing this test means to observe.
	done := make(chan struct{})

	var asked []string
	ask := func(_ context.Context, question string, level int, _ bool) error {
		asked = append(asked, question)
		s.Deliver("ok", level)
		if len(asked) == 2 {
			close(done)
		}
		return nil
	}

	l := &interfaceLoop{session: s, ask: ask, group: newGroup()}
	l.start(context.Background(), "the first question", false)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the queued follow-up was never sent")
	}

	if err := l.group.Close(); err != nil {
		t.Fatalf("group.Close: %v", err)
	}

	if len(asked) != 2 || asked[0] != "the first question" || asked[1] != "the queued follow-up" {
		t.Errorf("asked = %v, want the first question followed by the queued one", asked)
	}
	if got := s.QueueLen(); got != 0 {
		t.Errorf("QueueLen() after the chain = %d, want 0", got)
	}
}

// TestStartDoesNotDrainTheQueueAfterAFailedTurn covers the other side of that same
// judgment call, named in start's own doc comment: a turn that came back with an
// error leaves the queue alone, so a reader who watched one fail is not surprised
// by a second request starting on top of it.
func TestStartDoesNotDrainTheQueueAfterAFailedTurn(t *testing.T) {
	s := New(Options{Model: "some/model"})
	s.Enqueue("should not be sent")

	calls := 0
	ask := func(_ context.Context, _ string, _ int, _ bool) error {
		calls++
		return errFailedTurn
	}

	l := &interfaceLoop{session: s, ask: ask, group: newGroup()}
	l.start(context.Background(), "the first question", false)
	if err := l.group.Close(); err != nil {
		t.Fatalf("group.Close: %v", err)
	}

	if calls != 1 {
		t.Errorf("ask was called %d times, want exactly 1", calls)
	}
	if got := s.QueueLen(); got != 1 {
		t.Errorf("QueueLen() after a failed turn = %d, want the queued prompt left alone", got)
	}
}

// TestSubmitSendsAPlainQuestionToTheModel covers the bug a reader hit from the very
// first version of this file: a line that is not a /command used to go through
// l.runner (cmd/orcli/dispatch.go's Run), which has always answered any non-command
// line with an empty Result and no error, so the line was discarded with nothing
// written and nothing sent. submit now tells the two apart itself and sends a
// plain question straight to l.ask, the way the HELO already does.
func TestSubmitSendsAPlainQuestionToTheModel(t *testing.T) {
	s := New(Options{Model: "some/model"})

	var asked string
	var gotSilent bool
	ran := false
	ask := func(_ context.Context, question string, level int, silent bool) error {
		asked = question
		gotSilent = silent
		s.Deliver("an answer", level)
		return nil
	}
	runner := func(context.Context, string) (Result, error) {
		ran = true
		return Result{}, nil
	}

	l := &interfaceLoop{session: s, runner: runner, ask: ask, group: newGroup()}
	s.Editor().SetText("what is the weather doing")
	l.submit(context.Background())
	if err := l.group.Close(); err != nil {
		t.Fatalf("group.Close: %v", err)
	}

	if ran {
		t.Error("a plain question went through l.runner, want it sent directly to ask")
	}
	if asked != "what is the weather doing" {
		t.Errorf("ask was called with %q, want the typed line", asked)
	}
	if gotSilent {
		t.Error("ask was called with silent = true, want false: a reader's own typed line is shown")
	}
	if last, ok := s.History().Up(""); !ok || last != "what is the weather doing" {
		t.Errorf("History().Up() = %q, %v, want the plain question recorded", last, ok)
	}
}

// TestSubmitSendsACommandLineThroughTheRunner covers the other side: a line
// IsCommand recognizes still goes through l.runner exactly as before, and is never
// sent to ask on its own (a command's own Result.Ask, when it sets one, still
// reaches ask through the existing path below, which this test does not exercise).
func TestSubmitSendsACommandLineThroughTheRunner(t *testing.T) {
	s := New(Options{Model: "some/model"})

	ranWith := ""
	runner := func(_ context.Context, line string) (Result, error) {
		ranWith = line
		return Result{Text: "ok"}, nil
	}
	asked := false
	ask := func(context.Context, string, int, bool) error {
		asked = true
		return nil
	}

	l := &interfaceLoop{session: s, runner: runner, ask: ask, group: newGroup()}
	s.Editor().SetText("/model some/model")
	l.submit(context.Background())
	if err := l.group.Close(); err != nil {
		t.Fatalf("group.Close: %v", err)
	}

	if ranWith != "/model some/model" {
		t.Errorf("runner was called with %q, want the typed command line", ranWith)
	}
	if asked {
		t.Error("ask was called for a command line, want it left to the runner alone")
	}
	if last, ok := s.History().Up(""); !ok || last != "/model some/model" {
		t.Errorf("History().Up() = %q, %v, want the command line recorded", last, ok)
	}
}

func plainPalette() Palette { return NewPalette(false, GroundDark, nil) }

func TestTheBottomBarCarriesIdentityAndApproval(t *testing.T) {
	got := RenderBottom("Session 1", true, false,
		"openrouter.ai", "stealth/space-bunny-alpha", "3", "ask")
	for _, want := range []string{"Session 1", "[Mouse]", "Provider: openrouter.ai", "Model: stealth/space-bunny-alpha", "Approval: ask", "Verbosity: 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("bottom bar does not carry %q: %q", want, got)
		}
	}
	if strings.Contains(got, "Mouse:") || strings.Index(got, "Approval") > strings.Index(got, "Verbosity") {
		t.Errorf("bottom bar has an invalid field order or mouse label: %q", got)
	}
}

func TestTheBottomBarLeavesOutWhatIsOff(t *testing.T) {
	got := RenderBottom("Session 1", false, false,
		"openrouter.ai", "stealth/space-bunny-alpha", "3", "ask")
	for _, unwanted := range []string{"[Mouse]", "[Copy]"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("bottom bar carries %q while off: %q", unwanted, got)
		}
	}
}

func TestTheTopBarCarriesItsFieldsInOrder(t *testing.T) {
	got := RenderTop("working", "3", "18,400", "2,104", "916", "$0.0181", "$4.82")
	wants := []string{"Status: working", "Reasoning: 3", "Context: 18,400", "In: 2,104", "Out: 916", "Cost: $0.0181", "Credits: $4.82"}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("top bar does not carry %q: %q", want, got)
		}
	}
	if head, _, ok := strings.Cut(got, " | "); ok && head != wants[0] {
		t.Errorf("top bar begins with %q, want Status", head)
	}
}

func TestBarsKeepEveryFieldAtEveryWidth(t *testing.T) {
	fields := []Field{{Name: "Credits", Value: "10"}, {Name: "Cost", Value: "1"}, {Name: "Context", Value: "20%"}}
	want := "Credits: 10 | Cost: 1 | Context: 20%"
	for _, width := range []int{0, 1, 12, 20, 24, 25, 40, 200} {
		if got := RenderBar(fields, width); got != want {
			t.Errorf("at width %d the bar is %q, want %q", width, got, want)
		}
	}
}

func TestEmptyBarFieldsAndStatusNotesStayReadable(t *testing.T) {
	bar := RenderBottom("Session 1", false, false, "", "", "", "")
	for _, want := range []string{"Provider: -", "Model: -", "Approval: -", "Verbosity: -"} {
		if !strings.Contains(bar, want) {
			t.Errorf("bottom bar does not carry %q: %q", want, bar)
		}
	}
	note := RenderTop("paused (buffered 12 rows)", "", "", "", "", "", "")
	if !strings.Contains(note, "Status: paused (buffered 12 rows)") || strings.Contains(note, "Note:") {
		t.Errorf("status note was rendered as a separate field: %q", note)
	}
}

func TestBarWithNoWidthIsNotCut(t *testing.T) {
	got := RenderBar([]Field{{Name: "Credits", Value: "10"}, {Name: "Host", Value: "localhost"}}, 0)
	if !strings.Contains(got, "Host") {
		t.Errorf("a bar with no width lost a field: %q", got)
	}
}

// TestCopyModeFreezesStatusAndSweep covers the toggle: while copy mode is on,
// repeated status() calls return an unchanged Status even as state that would
// normally move it changes (a turn running, a row appended), and paint() stops
// advancing the sweep step. Turning copy mode back off resumes tracking live
// state immediately.
func TestCopyModeFreezesStatusAndSweep(t *testing.T) {
	s := New(Options{Model: "some/model"})
	l := &interfaceLoop{session: s, frame: NewFrame(), group: newGroup()}

	s.SetCopyMode(true)

	l.paint()
	stepAfterFirst := l.step
	first := l.status(StateThinking, "", "thinking")

	// A turn "running" would otherwise advance the sweep and change fieldHeld
	// by appending rows; with copy mode on, neither should move.
	s.Notice("a row that would otherwise move fieldHeld", 0, RoleNotice)
	l.paint()
	if l.step != stepAfterFirst {
		t.Errorf("l.step moved from %d to %d while copy mode was on", stepAfterFirst, l.step)
	}

	second := l.status(StateThinking, "", "thinking")
	if first != second {
		t.Errorf("status() changed while copy mode was on:\nfirst:  %+v\nsecond: %+v", first, second)
	}

	// Turning copy mode off resumes live tracking: fieldHeld must now reflect
	// the row that was appended while it was frozen.
	s.SetCopyMode(false)
	third := l.status(StateThinking, "", "thinking")
	if third[fieldHeld] == first[fieldHeld] && third == first {
		t.Errorf("status() stayed frozen after copy mode turned off")
	}
}

func stripSequences(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1b {
			i += escapeLength(runes[i+1:])
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}
