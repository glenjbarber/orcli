package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
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
// one path send a request with no model while another refuses it.
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
	s.log.Append(Row{Text: "a delegate question", Level: 3, Kind: KindQuestion})
	s.log.Append(Row{Text: "a delegate answer", Level: 3, Kind: KindReply})

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

// TestRowsAtAnAbsentLevelIsEmpty rather than an error. A reader typing a level that
// is not there should be told there is nothing there, not that the command was wrong.
func TestRowsAtAnAbsentLevelIsEmpty(t *testing.T) {
	s := New(Options{Model: "some/model"})
	s.log.Append(Row{Text: "a row", Level: 0})

	if got := s.RowsAt(9); len(got) != 0 {
		t.Errorf("level 9 gave %d rows, want none", len(got))
	}
}

// TestLevelsAreOrderedByValue covers the list a reader types from. It is ordered by
// value rather than by first appearance, so it does not reorder as a turn runs.
//
// Level 0 is always present: the opening banner carries it, and a session whose main
// conversation has not yet asked anything still has a level 0 to copy.
func TestLevelsAreOrderedByValue(t *testing.T) {
	s := New(Options{Model: "some/model"})

	s.log.Append(Row{Text: "a", Level: 7})
	s.log.Append(Row{Text: "b", Level: 1})
	s.log.Append(Row{Text: "c", Level: 4})
	s.log.Append(Row{Text: "d", Level: 4})

	got := s.Levels()
	want := []int{0, 1, 4, 7}
	if len(got) != len(want) {
		t.Fatalf("Levels gave %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Levels gave %v, want %v", got, want)
		}
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
