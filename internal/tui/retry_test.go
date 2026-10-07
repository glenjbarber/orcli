package tui

import (
	"context"
	"errors"
	"testing"
)

// TestRetryWithBackoffRetriesTransientFailures covers the budget: an attempt
// that keeps reporting a TransientError is tried retryBound times in all,
// and the last underlying failure is what comes back once the budget is
// spent.
func TestRetryWithBackoffRetriesTransientFailures(t *testing.T) {
	want := errors.New("stalled")
	attempts := 0

	err := retryWithBackoff(context.Background(), func() error {
		attempts++
		return Transient(want)
	})

	if attempts != retryBound {
		t.Fatalf("attempts = %d, want %d", attempts, retryBound)
	}
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// TestRetryWithBackoffStopsOnPermanentFailure covers the other half of the
// rule: an error an attempt did not mark transient ends the retry on the
// first attempt, since retrying a refusal is a delay rather than a retry.
func TestRetryWithBackoffStopsOnPermanentFailure(t *testing.T) {
	want := errors.New("refused")
	attempts := 0

	err := retryWithBackoff(context.Background(), func() error {
		attempts++
		return want
	})

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// TestRetryWithBackoffSucceedsAfterTransientFailures covers the case a
// stall clears before the budget is spent: the caller sees success and
// nothing about the earlier failures.
func TestRetryWithBackoffSucceedsAfterTransientFailures(t *testing.T) {
	attempts := 0

	err := retryWithBackoff(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return Transient(errors.New("stalled"))
		}
		return nil
	})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

// TestRetryWithBackoffStopsOnContextCancel covers a reader stopping the
// surrounding turn: the backoff wait has to end with it rather than block
// on work nobody wants an answer to anymore.
func TestRetryWithBackoffStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	err := retryWithBackoff(ctx, func() error {
		attempts++
		if attempts == 1 {
			cancel()
		}
		return Transient(errors.New("stalled"))
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

// TestTransientWrapsNilToNil covers the convenience the doc comment
// promises: a caller can wrap an attempt's error unconditionally.
func TestTransientWrapsNilToNil(t *testing.T) {
	if Transient(nil) != nil {
		t.Fatalf("Transient(nil) = %v, want nil", Transient(nil))
	}
}

// TestSpawnRetryingBehavesLikeSpawnOnPermanentFailure covers Spawn's own
// failures: none of them are transient, so SpawnRetrying returns on the
// first attempt and records a notice, the same way a caller of Spawn
// directly would see the refusal.
func TestSpawnRetryingBehavesLikeSpawnOnPermanentFailure(t *testing.T) {
	s := New(Options{Model: "some/model"})

	_, err := s.SpawnRetrying(context.Background(), 0, "")
	if !errors.Is(err, ErrNoQuestion) {
		t.Fatalf("err = %v, want ErrNoQuestion", err)
	}

	rows := s.Log().Rows()
	last := rows[len(rows)-1]
	if last.Kind != KindNotice {
		t.Fatalf("last row kind = %v, want KindNotice", last.Kind)
	}
}

// TestSpawnRetryingSucceeds covers the ordinary path: a question that
// Spawn accepts comes back as a worker, with nothing retried and nothing
// noticed.
func TestSpawnRetryingSucceeds(t *testing.T) {
	s := New(Options{Model: "some/model"})

	w, err := s.SpawnRetrying(context.Background(), 0, "a question")
	if err != nil {
		t.Fatalf("SpawnRetrying: %v", err)
	}
	if w == nil {
		t.Fatal("w = nil, want a worker")
	}
}

// TestDeliverHandoffRetriesAndNotices covers the reusable helper /begin's
// own handoff delivery will call: it retries a transient delivery failure
// on the shared budget and, once exhausted, records a notice rather than
// failing silently.
func TestDeliverHandoffRetriesAndNotices(t *testing.T) {
	s := New(Options{Model: "some/model"})
	attempts := 0

	err := s.DeliverHandoff(context.Background(), 0, func(context.Context) error {
		attempts++
		return Transient(errors.New("delivery stalled"))
	})

	if attempts != retryBound {
		t.Fatalf("attempts = %d, want %d", attempts, retryBound)
	}
	if err == nil {
		t.Fatal("err = nil, want the stalled delivery error")
	}

	rows := s.Log().Rows()
	last := rows[len(rows)-1]
	if last.Kind != KindNotice {
		t.Fatalf("last row kind = %v, want KindNotice", last.Kind)
	}
}
