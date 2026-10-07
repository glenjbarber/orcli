package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSessionOpensOnARow covers what a reader sees first. A session that opens onto
// an empty screen gives nothing to tell it has started.
func TestSessionOpensOnARow(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if got := s.Log().Len(); got != 1 {
		t.Fatalf("the log holds %d rows at startup, want 1", got)
	}
	if got := s.Log().Rows()[0].Kind; got != KindNotice {
		t.Errorf("the opening row is kind %d, want a notice", got)
	}
}

// TestNewSessionIsIdle covers the state a reader finds before typing. Anything else
// tells them something is running when nothing is.
func TestNewSessionIsIdle(t *testing.T) {
	state, detail := New(Options{Model: "some/model"}).State()

	if state != StateIdle {
		t.Errorf("a new session is %q, want idle", state)
	}
	if detail != "" {
		t.Errorf("a new session carries detail %q, want none", detail)
	}
}

// TestReadyRefusesNoModel covers the check that lives in one place. Every caller that
// starts a turn asks this rather than looking at the model, so a session cannot let
// one path send a request with no model while another refuse it.
func TestReadyRefusesNoModel(t *testing.T) {
	s := New(Options{})
	if err := s.Ready(); !errors.Is(err, ErrNoModel) {
		t.Errorf("Ready returned %v, want ErrNoModel", err)
	}

	s = New(Options{Model: "some/model"})
	if err := s.Ready(); err != nil {
		t.Errorf("Ready returned %v, want nil", err)
	}
}

// TestBeginRefusesAnEmptyQuestion covers the case where a turn would run with nothing
// to answer.
func TestBeginRefusesAnEmptyQuestion(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if _, err := s.Begin(context.Background(), "", 0); err == nil {
		t.Fatal("an empty question started a turn, want a refusal")
	}
	if got := s.Log().Len(); got != 1 {
		t.Errorf("the log holds %d rows, want 1: a refused turn records nothing", got)
	}
}

// TestBeginRefusesWithNoModel covers the order of the checks. A session with no model
// and an empty question is refused for the model, since that is the one the reader
// has to fix before the question matters.
func TestBeginRefusesWithNoModel(t *testing.T) {
	s := New(Options{})

	_, err := s.Begin(context.Background(), "", 0)
	if !errors.Is(err, ErrNoModel) {
		t.Errorf("Begin returned %v, want ErrNoModel", err)
	}
}

// TestBeginEchoesTheQuestion covers the row a reader needs above the answer. A log
// that opened straight onto a reply gives no way to tell which question it answered.
func TestBeginEchoesTheQuestion(t *testing.T) {
	s := New(Options{Model: "some/model"})
	if _, err := s.Begin(context.Background(), "what does the log do?", 0); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	rows := s.Log().Rows()
	last := rows[len(rows)-1]
	if last.Kind != KindQuestion {
		t.Errorf("the row is kind %d, want a question", last.Kind)
	}
	if last.Text != "what does the log do?" {
		t.Errorf("the row is %q, want the question as written", last.Text)
	}
}

// TestBeginSilentWritesNoRow covers BeginSilent's one difference from Begin: the
// question it is given reaches the model (the turn still starts, with the same
// readiness check and state transition Begin gives), but it never appears in the
// log as a question row the reader would read as something they typed.
func TestBeginSilentWritesNoRow(t *testing.T) {
	s := New(Options{Model: "some/model"})
	before := s.Log().Len()

	if _, err := s.BeginSilent(context.Background(), "greet the reader", 0); err != nil {
		t.Fatalf("BeginSilent: %v", err)
	}

	if got := s.Log().Len(); got != before {
		t.Errorf("the log holds %d rows after BeginSilent, want %d: no question row should be written", got, before)
	}
	if s.state != StateThinking {
		t.Errorf("session state = %v, want StateThinking: BeginSilent should still start the turn", s.state)
	}
}

// TestBeginSilentRefusesLikeBegin covers that BeginSilent shares Begin's
// readiness and empty-question checks rather than skipping them along with the
// row: a silent turn is still a turn, and must fail exactly where a normal one
// would.
func TestBeginSilentRefusesLikeBegin(t *testing.T) {
	s := New(Options{})

	if _, err := s.BeginSilent(context.Background(), "greet the reader", 0); !errors.Is(err, ErrNoModel) {
		t.Errorf("BeginSilent returned %v, want ErrNoModel", err)
	}

	s = New(Options{Model: "some/model"})
	if _, err := s.BeginSilent(context.Background(), "", 0); !errors.Is(err, ErrNoQuestion) {
		t.Errorf("BeginSilent returned %v, want ErrNoQuestion", err)
	}
}

// TestThinkingBecomesWorkingOnTheFirstDelivery covers the transition that makes both
// states worth having. Before the first delivery the reader has no evidence a turn
// is running, and after it they do.
func TestThinkingBecomesWorkingOnTheFirstDelivery(t *testing.T) {
	s := New(Options{Model: "some/model"})
	if _, err := s.Begin(context.Background(), "a question", 0); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	if state, _ := s.State(); state != StateThinking {
		t.Errorf("after Begin the state is %q, want thinking", state)
	}

	s.Deliver("the first words", 0)
	if state, _ := s.State(); state != StateWorking {
		t.Errorf("after the first delivery the state is %q, want working", state)
	}

	s.Deliver(" and the rest", 0)
	if state, _ := s.State(); state != StateWorking {
		t.Errorf("after a second delivery the state is %q, want working", state)
	}
}

// TestDeliverHoldsTheTextWhole covers the decision the interface was redesigned for.
// A reply arrives as one row, so folding and the copy path see the same text the
// reader does and a log does not grow a row per token.
func TestDeliverHoldsTheTextWhole(t *testing.T) {
	s := New(Options{Model: "some/model"})
	if _, err := s.Begin(context.Background(), "a question", 0); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	const answer = "a whole reply, in one row, with a newline\nand a second line"
	s.Deliver(answer, 0)

	rows := s.Log().Rows()
	last := rows[len(rows)-1]
	if last.Kind != KindReply {
		t.Errorf("the row is kind %d, want a reply", last.Kind)
	}
	if last.Text != answer {
		t.Errorf("the row is %q, want the reply as delivered", last.Text)
	}
}

// TestDeliverIgnoresNothing covers the case where a turn produced no text, such as a
// turn that went straight to a tool call. A row with no text in it is a blank line the
// reader has to scroll past.
func TestDeliverIgnoresNothing(t *testing.T) {
	s := New(Options{Model: "some/model"})
	before := s.Log().Len()

	s.Deliver("", 0)
	if got := s.Log().Len(); got != before {
		t.Errorf("an empty delivery added a row: %d became %d", before, got)
	}
}

// TestFinishedReturnsToIdleWhateverTheReason covers the case where a turn was stopped
// rather than completed. A reader who stopped a model should not be told something is
// still running.
func TestFinishedReturnsToIdleWhateverTheReason(t *testing.T) {
	for _, reason := range []string{"done", "stopped", "failed: something"} {
		s := New(Options{Model: "some/model"})
		if _, err := s.Begin(context.Background(), "a question", 0); err != nil {
			t.Fatalf("Begin: %v", err)
		}
		s.Deliver("an answer", 0)

		s.Finished(reason)

		if state, _ := s.State(); state != StateIdle {
			t.Errorf("after finishing with %q the state is %q, want idle", reason, state)
		}
		rows := s.Log().Rows()
		if got := rows[len(rows)-1].Text; got != reason {
			t.Errorf("the last row is %q, want %q", got, reason)
		}
	}
}

// TestRowsAtCoversEveryRowOfALevel covers what `/copy N` copies. It is a level and not
// a row number, since a fold changes how many rows there are and a reader who counted
// them would be counting something that moves.
func TestRowsAtCoversEveryRowOfALevel(t *testing.T) {
	s := New(Options{Model: "some/model"})

	s.log.Append(Row{Text: "the main question", Level: 0, Kind: KindQuestion})
	s.log.Append(Row{Text: "the main answer", Level: 0, Kind: KindReply})
	s.log.Append(Row{Text: "a thread question", Level: 3, Kind: KindQuestion})
	s.log.Append(Row{Text: "a thread answer", Level: 3, Kind: KindReply})

	got := s.RowsAt(3)
	if len(got) != 2 {
		t.Fatalf("level 3 has %d rows, want 2", len(got))
	}
	for _, row := range got {
		if row.Level != 3 {
			t.Errorf("a row at level %d was returned for level 3", row.Level)
		}
	}
}

// TestRowsAtAnAbsentLevelIsEmpty rather than an error, since RowsAt is the raw view
// and CopyLevel is the one that reports what a level was.
func TestRowsAtAnAbsentLevelIsEmpty(t *testing.T) {
	s := New(Options{Model: "some/model"})
	s.log.Append(Row{Text: "a row", Level: 0})

	if got := len(s.RowsAt(9)); got != 0 {
		t.Errorf("level 9 gave %d rows, want none", got)
	}
}

// TestNoticeCarriesItsRole covers the route a failure takes. A notice written without a
// role is a notice every reader has to read the same way, which is the one thing colour
// and weight are for.
func TestNoticeCarriesItsRole(t *testing.T) {
	s := New(Options{Model: "some/model"})

	s.Notice("the reader declined a program", 0, RoleFailure)

	rows := s.Log().Rows()
	last := rows[len(rows)-1]
	if len(last.Spans) != 1 {
		t.Fatalf("the notice carries %d spans, want 1", len(last.Spans))
	}
	if got := last.Spans[0].Role; got != RoleFailure {
		t.Errorf("the span role is %d, want a failure", got)
	}
}

// TestConcurrentTurnsAndNotices covers the concurrency the session is built for. A turn
// goroutine delivers while the input goroutine is writing notices, and the log is the
// one thing both of them touch.
func TestConcurrentTurnsAndNotices(t *testing.T) {
	s := New(Options{Model: "some/model"})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range 500 {
			s.Deliver("an answer", 0)
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			s.Notice("a notice", 0, RoleNotice)
		}
	}()

	wg.Wait()

	// 1 opening row, 500 answers, 500 notices.
	if got, want := s.Log().Len(), 1001; got != want {
		t.Errorf("the log holds %d rows, want %d", got, want)
	}
}

// TestConcurrentLevelsAndRows covers the two locks separately. The levels table has
// its own, since a level is asked about while a turn is running and holding the state
// lock for that would block the painter.
func TestConcurrentLevelsAndRows(t *testing.T) {
	s := New(Options{Model: "some/model"})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range 200 {
			if _, err := s.OpenLevel(0, "a thread"); err != nil {
				t.Errorf("OpenLevel: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			s.Deliver("an answer", 0)
		}
	}()

	wg.Wait()

	// 200 threads on top of the root.
	if got, want := len(s.Levels()), 201; got != want {
		t.Errorf("the session has %d levels, want %d", got, want)
	}
}

// TestTheCredentialIsNotInTheLog covers the rule that matters most. A log is a thing a
// reader selects out of and pastes somewhere else, so a credential reaching one would
// be a credential in the clipboard.
func TestTheCredentialIsNotInTheLog(t *testing.T) {
	const key = "sk-or-v1-secret"
	s := New(Options{APIKey: key, Model: "some/model"})

	if _, err := s.Begin(context.Background(), "a question", 0); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	s.Deliver("an answer", 0)
	s.Notice("a notice", 0, RoleNotice)
	s.Finished("done")

	for _, row := range s.Log().Rows() {
		if strings.Contains(row.Text, key) {
			t.Errorf("the log carries the credential: %q", row.Text)
		}
	}

	if !strings.Contains(s.Options().APIKey, key) {
		t.Error("the session lost the credential, which it needs for a request")
	}
}

// TestScrollStartsAtTheLiveEdge covers the zero value: a session nothing has scrolled
// yet is at the live edge, which is what bar two's scrollback field reads.
func TestScrollStartsAtTheLiveEdge(t *testing.T) {
	s := New(Options{})
	if !s.AtLiveEdge() {
		t.Error("a fresh session is not at the live edge")
	}
	if got := s.ScrollOffset(); got != 0 {
		t.Errorf("a fresh session's scroll offset is %d, want 0", got)
	}
}

// TestScrollUpLeavesTheLiveEdgeAndScrollDownReturns covers Shift+Up/Shift+Down's effect
// directly on Session, without a key event.
func TestScrollUpLeavesTheLiveEdgeAndScrollDownReturns(t *testing.T) {
	s := New(Options{})
	for i := range 5 {
		s.Notice(fmt.Sprintf("row %d", i), 0, RoleNotice)
	}

	s.ScrollUp(2)
	if s.AtLiveEdge() {
		t.Error("scrolling up left the session at the live edge")
	}
	if got := s.ScrollOffset(); got != 2 {
		t.Errorf("scroll offset after ScrollUp(2) is %d, want 2", got)
	}

	s.ScrollDown(2)
	if !s.AtLiveEdge() {
		t.Error("scrolling back down by the same amount did not return to the live edge")
	}
}

// TestScrollClampsAtBothEnds covers the two floors: scrolling down past the live edge
// stays at it, and scrolling up past the log's own length stops there rather than
// reading rows that do not exist.
func TestScrollClampsAtBothEnds(t *testing.T) {
	s := New(Options{})
	for i := range 3 {
		s.Notice(fmt.Sprintf("row %d", i), 0, RoleNotice)
	}

	s.ScrollDown(1)
	if got := s.ScrollOffset(); got != 0 {
		t.Errorf("scrolling down from the live edge gives %d, want 0", got)
	}

	s.ScrollUp(100)
	if got, want := s.ScrollOffset(), s.Log().Len(); got != want {
		t.Errorf("scrolling up past the log's length gives %d, want the log's own length %d", got, want)
	}
}

// TestBreakDueWaitsOutTheInterval covers the ordinary case: a break is not
// due before the configured interval has passed, and is due once it has.
func TestBreakDueWaitsOutTheInterval(t *testing.T) {
	s := New(Options{BreakInterval: time.Minute})

	if s.BreakDue(time.Now()) {
		t.Error("a new session is already due a break")
	}

	s.lastBreak = time.Now().Add(-2 * time.Minute)
	if !s.BreakDue(time.Now()) {
		t.Error("a session whose interval has passed is not due a break")
	}
}

// TestBreakDueIsOffByDefault covers a session built with no BreakInterval:
// the zero value turns the reminder off rather than firing immediately.
func TestBreakDueIsOffByDefault(t *testing.T) {
	s := New(Options{})
	s.lastBreak = time.Now().Add(-24 * time.Hour)

	if s.BreakDue(time.Now()) {
		t.Error("a session with no BreakInterval is due a break")
	}
}

// TestBreakDueWaitsForIdle covers the rule that a break does not interrupt a
// turn in flight. The interval having passed is not enough on its own.
func TestBreakDueWaitsForIdle(t *testing.T) {
	s := New(Options{Model: "some/model", BreakInterval: time.Minute})
	s.lastBreak = time.Now().Add(-2 * time.Minute)
	s.SetState(StateThinking, "")

	if s.BreakDue(time.Now()) {
		t.Error("a break is due while a turn is thinking")
	}
}

// TestStartBreakClearsTheEditorAndRecordsANotice covers the approved
// interaction: the prompt goes blank and the reader is told why.
func TestStartBreakClearsTheEditorAndRecordsANotice(t *testing.T) {
	s := New(Options{})
	s.Editor().Insert('a')
	before := s.Log().Len()

	s.StartBreak(time.Now())

	state, detail := s.State()
	if state != StateBreak {
		t.Errorf("state is %q, want %q", state, StateBreak)
	}
	if detail == "" {
		t.Error("StartBreak left no countdown in the detail")
	}
	if !s.Editor().Empty() {
		t.Error("StartBreak left text in the editor")
	}
	if got := s.Log().Len(); got != before+1 {
		t.Errorf("the log holds %d rows after StartBreak, want %d", got, before+1)
	}
}

// TestTickBreakCountsDownThenEnds covers the two things a tick does while a
// break is running: update the countdown, and end the break once the
// duration has passed, returning the session to idle and recording the
// interval was reset from that moment.
func TestTickBreakCountsDownThenEnds(t *testing.T) {
	s := New(Options{})
	start := time.Now()
	s.StartBreak(start)

	s.TickBreak(start.Add(30 * time.Second))
	if state, _ := s.State(); state != StateBreak {
		t.Fatalf("state after a partial tick is %q, want %q", state, StateBreak)
	}

	end := start.Add(breakDuration + time.Second)
	s.TickBreak(end)

	state, detail := s.State()
	if state != StateIdle {
		t.Errorf("state after the break ends is %q, want %q", state, StateIdle)
	}
	if detail != "" {
		t.Errorf("detail after the break ends is %q, want empty", detail)
	}
	if !s.lastBreak.Equal(end) {
		t.Errorf("lastBreak is %v, want %v", s.lastBreak, end)
	}
}

// TestTickBreakHalvesOnceWhenTheReaderKeptTyping covers a reader who never
// stopped: the first close does not end the break or say so, it halves the
// remaining time and tries once more.
func TestTickBreakHalvesOnceWhenTheReaderKeptTyping(t *testing.T) {
	s := New(Options{})
	start := time.Now()
	s.StartBreak(start)
	before := s.Log().Len()

	s.NoteBreakActivity()
	end := start.Add(breakDuration + time.Second)
	s.TickBreak(end)

	state, detail := s.State()
	if state != StateBreak {
		t.Fatalf("state after the first close is %q, want %q", state, StateBreak)
	}
	if detail == "" {
		t.Error("the halved break left no countdown in the detail")
	}
	if got := s.Log().Len(); got != before+1 {
		t.Errorf("the halved break logged %d rows, want 1", got-before)
	}
	if rows := s.Log().Rows(); strings.Contains(rows[len(rows)-1].Text, "ended") {
		t.Error("the halved break's own notice says the break ended")
	}
	if !s.breakEnd.Equal(end.Add(breakDuration / 2)) {
		t.Errorf("breakEnd after halving is %v, want %v", s.breakEnd, end.Add(breakDuration/2))
	}
}

// TestTickBreakEndsSilentlyOnTheSecondCloseIfStillTyping covers the rest of
// that reader's break: the halved period gets no second halving, and still
// says nothing about ending if they kept typing through it too, but does
// reset lastBreak so the next full interval starts from here.
func TestTickBreakEndsSilentlyOnTheSecondCloseIfStillTyping(t *testing.T) {
	s := New(Options{})
	start := time.Now()
	s.StartBreak(start)
	s.NoteBreakActivity()
	firstClose := start.Add(breakDuration + time.Second)
	s.TickBreak(firstClose)

	s.NoteBreakActivity()
	before := s.Log().Len()
	secondClose := firstClose.Add(breakDuration/2 + time.Second)
	s.TickBreak(secondClose)

	state, _ := s.State()
	if state != StateIdle {
		t.Errorf("state after the second close is %q, want %q", state, StateIdle)
	}
	if got := s.Log().Len(); got != before {
		t.Errorf("the second close logged %d rows, want 0", got-before)
	}
	if !s.lastBreak.Equal(secondClose) {
		t.Errorf("lastBreak is %v, want %v", s.lastBreak, secondClose)
	}
}

// TestTickBreakEndsNormallyOnTheSecondCloseIfInputStopped covers a reader
// who took the hint partway through: no further typing during the halved
// period gets the ordinary "ended" notice back, same as a break nobody
// interrupted at all.
func TestTickBreakEndsNormallyOnTheSecondCloseIfInputStopped(t *testing.T) {
	s := New(Options{})
	start := time.Now()
	s.StartBreak(start)
	s.NoteBreakActivity()
	firstClose := start.Add(breakDuration + time.Second)
	s.TickBreak(firstClose)

	before := s.Log().Len()
	secondClose := firstClose.Add(breakDuration/2 + time.Second)
	s.TickBreak(secondClose)

	if got := s.Log().Len(); got != before+1 {
		t.Fatalf("the second close logged %d rows, want 1", got-before)
	}
	if rows := s.Log().Rows(); !strings.Contains(rows[len(rows)-1].Text, "ended") {
		t.Error("the second close did not say the break ended")
	}
}

// TestNoteBreakActivityIsANoOpOutsideABreak covers the one thing that lets
// every key reach it unconditionally: it does nothing unless a break is
// actually running.
func TestNoteBreakActivityIsANoOpOutsideABreak(t *testing.T) {
	s := New(Options{})
	s.NoteBreakActivity()
	if s.breakInput {
		t.Error("NoteBreakActivity set breakInput with no break running")
	}
}

// TestTickBreakDoesNothingOutsideABreak covers a tick arriving while no
// break is running, which is the ordinary case on every repaint.
func TestTickBreakDoesNothingOutsideABreak(t *testing.T) {
	s := New(Options{Model: "some/model"})
	before := s.Log().Len()

	s.TickBreak(time.Now())

	if state, _ := s.State(); state != StateIdle {
		t.Errorf("state is %q, want %q", state, StateIdle)
	}
	if got := s.Log().Len(); got != before {
		t.Errorf("TickBreak with no break running appended a row")
	}
}
