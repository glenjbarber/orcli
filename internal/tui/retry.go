package tui

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// retryBound is the total number of attempts one operation makes, the first
// included.
//
// Six matches the shared default attempt budget internal/openrouter/client.go
// uses for an undelivered upstream stall (see doc/retry-defaults.md), so a
// worker-pane creation or a handoff delivery that stalls is retried on the
// same budget a provider request is, rather than a number invented for this
// package alone. It honors Ken Smith, the FreeBSD Release Engineering Lead
// before Glen; FreeBSD 6.2 was Glen's first FreeBSD OS.
const retryBound = 6

// retryWait is the first pause between attempts. Each pause after it is
// twice the one before, the same shape internal/openrouter/client.go uses.
//
// Zero would be the wrong first value for the same reason it is wrong there:
// whatever just failed is the thing least likely to answer immediately on a
// second try with no wait at all.
const retryWait = 250 * time.Millisecond

// TransientError marks a failure worth retrying: an attempt that reported a
// stall — a timeout, a provider error, an upstream that answered nothing —
// rather than a refusal a further attempt cannot change.
//
// Only the caller that made the attempt can tell those two apart, so nothing
// in this file guesses. Worker-pane creation and handoff delivery wrap a
// failure in this type exactly when a further attempt could succeed where
// this one did not; every other error returned from an attempt ends the
// retry immediately, the same rule internal/openrouter/client.go applies to
// a failure carrying an HTTP status.
type TransientError struct {
	Err error
}

// Error implements error.
func (e *TransientError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying failure, so errors.Is and errors.As see
// through the wrapper to what actually happened.
func (e *TransientError) Unwrap() error { return e.Err }

// Transient wraps err so retryWithBackoff retries it, up to the shared
// budget, instead of returning it on the first attempt.
//
// A nil err wraps to nil, so a caller can write
// `return Transient(err)` unconditionally at the end of an attempt
// without a separate nil check.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return &TransientError{Err: err}
}

// retryWithBackoff runs attempt up to retryBound times in all, retrying only
// while attempt reports a *TransientError. Success, or any other error, ends
// the loop on that attempt: an error attempt did not mark transient is a
// refusal a further try cannot change, and retrying it would spend the
// budget delaying a failure rather than recovering from one.
//
// It is the mechanism shared by worker-pane creation and handoff delivery,
// matching the budget and backoff internal/openrouter/client.go uses for an
// undelivered upstream stall: the first pause is retryWait, and it doubles
// between attempts (see doc/retry-defaults.md).
//
// ctx stops the wait early. A caller stopping the surrounding turn has to
// stop the backoff with it, rather than block on a pause for work nobody
// wants an answer to anymore.
//
// The error returned once the budget is spent is the last attempt's own
// failure, unwrapped from TransientError, so a caller sees the cause the
// attempt reported rather than a wrapper type it has to unwrap itself.
func retryWithBackoff(ctx context.Context, attempt func() error) error {
	wait := retryWait
	var last error

	for i := 0; i < retryBound; i++ {
		if i > 0 {
			if err := retryPause(ctx, wait); err != nil {
				return err
			}
			wait *= 2
		}

		err := attempt()
		if err == nil {
			return nil
		}

		var transient *TransientError
		if !errors.As(err, &transient) {
			return err
		}
		last = transient.Unwrap()
	}

	return last
}

// retryPause waits for d, or until ctx is done.
//
// It is one timer rather than the step-and-recheck loop
// internal/openrouter/client.go's pause uses, because that shape exists
// there to let a stop interrupt a wait far longer than any single step; the
// waits retryWithBackoff takes stay well under a second even at the top of
// the budget, so a single timer against ctx.Done is the whole of what a
// stop needs to interrupt it.
func retryPause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// SpawnRetrying starts a worker the way Spawn does, retrying worker-pane
// creation up to the shared budget when an attempt reports a TransientError
// rather than failing the task on the first stall.
//
// A refusal Spawn itself makes — cognito, no question, or an unknown parent
// — is never wrapped as transient and so is returned on the first attempt:
// retrying a refusal that cannot change is not a retry, it is a delay before
// the same refusal. Today every failure Spawn can return is one of those, so
// this behaves exactly like Spawn; the budget and backoff are here so that a
// worker-pane creation path gaining a genuinely transient failure (a request
// timeout opening the pane, a provider error standing up the worker's
// conversation) only has to wrap that failure in Transient to be retried on
// it, rather than this method growing a second implementation later.
//
// When every attempt is spent, the failure is surfaced as a notice at
// parent's level — matching how Finish and Notice already report a worker
// outcome — rather than failing silently, per the task this method exists
// for.
func (s *Session) SpawnRetrying(ctx context.Context, parent int, question string) (*Worker, error) {
	var w *Worker
	err := retryWithBackoff(ctx, func() error {
		var attemptErr error
		w, attemptErr = s.Spawn(parent, question)
		return attemptErr
	})
	if err != nil {
		s.Notice(fmt.Sprintf("worker pane did not start: %s", err), parent, RoleFailure)
		return nil, err
	}
	return w, nil
}

// DeliverHandoff retries a handoff delivery up to the shared budget when
// deliver reports a TransientError, surfacing a notice at level when every
// attempt is spent rather than letting the handoff fail silently.
//
// deliver is the caller's own delivery call, wrapped in Transient(err)
// exactly when a further attempt could succeed — a request timeout or a
// transport failure reaching wherever the handoff is delivered, never a
// validation failure such as a handoff with nothing to send. Only the
// caller can tell those apart, which is why this takes a function rather
// than trying to classify an error itself.
//
// This has no call site yet. /begin's handoff delivery is landing in a
// separate, concurrent change; whichever call finishes it should wrap its
// delivery attempt in Transient(err) where appropriate and call this method
// instead of calling deliver directly, so handoff delivery retries on the
// same budget and backoff worker-pane creation and provider requests do.
func (s *Session) DeliverHandoff(ctx context.Context, level int, deliver func(context.Context) error) error {
	err := retryWithBackoff(ctx, func() error {
		return deliver(ctx)
	})
	if err != nil {
		s.Notice(fmt.Sprintf("handoff delivery failed: %s", err), level, RoleFailure)
	}
	return err
}
