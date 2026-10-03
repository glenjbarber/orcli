package openrouter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// retryScript serves one scripted stream per request, in order.
//
// A response is scripted rather than generated so a test states what the
// endpoint did rather than what the parser should conclude from it. Once the
// script runs out the last entry is repeated, so a test that expects a bound to
// stop the retries does not have to pad the script.
func retryScript(t *testing.T, replies ...string) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1)) - 1
		if n >= len(replies) {
			n = len(replies) - 1
		}

		w.Header().Set("Content-Type", "text/event-stream")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		if _, err := w.Write([]byte(replies[n])); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

// stallPayload is the payload an upstream stall is reported as.
//
// This is the shape the endpoint sends: an error object and nothing else. A
// parser reading only the choices and the accounting would decode it to nothing,
// report nothing for it, and go on to blame the missing terminating marker, which
// names a symptom rather than the cause.
func stallPayload(message string) string {
	return `{"error":{"message":` + quote(message) + `}}`
}

// retryClient returns a Client pointed at a test server.
//
// The base URL is set directly rather than through a constructor, since the
// endpoint is the test's own server and the constructor fixes the host.
func retryClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c := New("sk-or-v1-retry-test")
	c.baseURL = baseURL
	return c
}

// retryEvents runs one request and reports what the reader was shown.
func retryEvents(t *testing.T, c *Client) []Event {
	t.Helper()

	var events []Event
	if err := c.Chat(t.Context(), Request{Stream: true}, func(e Event) {
		events = append(events, e)
	}); err != nil {
		t.Fatalf("Chat returned %v, want nil: a failure reaches the reader as an event", err)
	}
	return events
}

// mentions reports whether a failure the reader saw carries a fragment.
//
// The provider's own wording is what a reader can search for, so the test looks
// for that rather than for a prefix this package chose.
func mentions(events []Event, fragment string) bool {
	for _, e := range events {
		if e.Kind == EventError && e.Err != nil &&
			strings.Contains(e.Err.Error(), fragment) {
			return true
		}
	}
	return false
}

// TestChatRetriesAStalledUpstream is the case the retry exists for.
//
// The endpoint reports the stall as a payload carrying nothing else and delivers
// no text, so nothing was spent and a second attempt cannot duplicate anything.
// The reader should see the reply rather than a failure.
func TestChatRetriesAStalledUpstream(t *testing.T) {
	srv, calls := retryScript(t,
		sse(stallPayload("Upstream idle timeout exceeded")),
		sse(chunkOf("the answer"), "[DONE]"),
	)
	events := retryEvents(t, retryClient(t, srv.URL))

	if got, want := calls.Load(), int64(2); got != want {
		t.Errorf("the endpoint was called %d times, want %d", got, want)
	}
	if got, want := deltas(events), "the answer"; got != want {
		t.Errorf("reply text = %q, want %q", got, want)
	}
	finish, ok := last(events, EventFinish)
	if !ok {
		t.Fatal("no finish event, want one")
	}
	if !finish.Finished {
		t.Error("Finished = false, want true: the retried request reached its marker")
	}
	if got := count(events, EventError); got != 0 {
		t.Errorf("the reader was told about %d failures, want 0: the retry succeeded", got)
	}
}

// TestChatStopsAtTheRetryBound covers the case the retry must not swallow: the
// endpoint fails the same way every time, so the bound is reached and the reader
// is told the real cause rather than a symptom of it.
func TestChatStopsAtTheRetryBound(t *testing.T) {
	srv, calls := retryScript(t, sse(stallPayload("Upstream idle timeout exceeded")))
	events := retryEvents(t, retryClient(t, srv.URL))

	if got, want := calls.Load(), int64(retryBound); got != want {
		t.Errorf("the endpoint was called %d times, want %d: the retry is bounded", got, want)
	}
	if !mentions(events, "idle timeout") {
		t.Errorf("the provider's message was not reported: %v", events)
	}
	finish, ok := last(events, EventFinish)
	if !ok {
		t.Fatal("no finish event, want one")
	}
	if finish.Finished {
		t.Error("Finished = true, want false: no reply ever arrived")
	}
}

// TestChatDoesNotRetryAPartialReply is the condition the retry depends on.
//
// A reply that has begun is a reply that was paid for, and a retry would either
// duplicate the text or throw away what the reader was reading. The existing
// handling stands: keep the partial, report the cause, finish unfinished.
func TestChatDoesNotRetryAPartialReply(t *testing.T) {
	srv, calls := retryScript(t,
		sse(chunkOf("half an "), stallPayload("Upstream idle timeout exceeded")),
	)
	events := retryEvents(t, retryClient(t, srv.URL))

	if got, want := calls.Load(), int64(1); got != want {
		t.Errorf("the endpoint was called %d times, want 1: a partial reply must not be retried", got)
	}
	if got, want := deltas(events), "half an "; got != want {
		t.Errorf("reply text = %q, want %q: the text that arrived is kept", got, want)
	}
	if !mentions(events, "idle timeout") {
		t.Errorf("the provider's message was not reported: %v", events)
	}
	finish, ok := last(events, EventFinish)
	if !ok {
		t.Fatal("no finish event, want one")
	}
	if finish.Finished {
		t.Error("Finished = true, want false: the stream was cut short")
	}
}

// TestChatReportsTheCauseOnce covers the case where the cause is known and the
// missing terminating marker would repeat it.
//
// The endpoint writes the stall into the stream and drops the connection. The
// reader is told the stall once, and not again as a missing marker, since a
// second report of a vaguer fault reads as though it were the whole of what
// went wrong.
func TestChatReportsTheCauseOnce(t *testing.T) {
	srv, _ := retryScript(t, sse(stallPayload("Upstream idle timeout exceeded")))
	events := retryEvents(t, retryClient(t, srv.URL))

	if got, want := count(events, EventError), 1; got != want {
		t.Errorf("the reader was told about the failure %d times, want %d", got, want)
	}
	if mentions(events, "terminating marker") {
		t.Errorf("the missing marker was reported beside a known cause: %v", events)
	}
}

// TestChatDoesNotRetryAServerError covers a stream that ends with no marker and
// nothing the endpoint called a failure.
//
// There is no cause to match on and nothing was delivered, so the request is
// reported as cut and is not made again: a reply whose fate is unknown is not the
// same as one known to have failed, and a silent second attempt would spend the
// allowance to learn nothing.
func TestChatDoesNotRetryAServerError(t *testing.T) {
	srv, calls := retryScript(t, "")
	events := retryEvents(t, retryClient(t, srv.URL))

	if got, want := calls.Load(), int64(1); got != want {
		t.Errorf("the endpoint was called %d times, want 1: no cause means no retry", got)
	}
	if !mentions(events, "terminating marker") {
		t.Errorf("the reader was not told the stream was cut: %v", events)
	}
}

// TestChatDoesNotRetryAnHTTPStatus covers the failure the retry must leave alone.
//
// A status is the endpoint refusing the request rather than an upstream stalling
// behind it, and a second request cannot satisfy a rejected credential or a
// malformed request. Retrying those would spend the allowance to be refused again.
func TestChatDoesNotRetryAnHTTPStatus(t *testing.T) {
	srv, calls := retryScript(t, sse("[DONE]"))

	c := retryClient(t, srv.URL)
	c.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Status:     "429 Too Many Requests",
			Body:       http.NoBody,
		}, nil
	})}

	events := retryEvents(t, c)

	if got, want := calls.Load(), int64(0); got != want {
		t.Errorf("the test server was called %d times, want %d", got, want)
	}
	if !mentions(events, "429") {
		t.Errorf("the status did not reach the reader: %v", events)
	}
	if got := count(events, EventFinish); got != 0 {
		t.Errorf("%d finish events, want 0: a refused request is not a cut stream", got)
	}
}

// roundTripFunc makes a function satisfy http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestChatRedactsTheCredentialInAReportedFailure covers the reason every
// diagnostic in this package goes through Filter, on the path the retry added.
//
// The credential travels in a header, so it is in no payload this client sends,
// but a proxy can quote it back in a failure, and a failure ends up in the
// terminal scrollback. The figure redacted is the one the client holds, since
// that is the only figure Filter can recognize.
func TestChatRedactsTheCredentialInAReportedFailure(t *testing.T) {
	const key = "sk-or-v1-retry-test"

	srv, _ := retryScript(t, sse(stallPayload("upstream said "+key)))
	events := retryEvents(t, retryClient(t, srv.URL))

	found := false
	for _, e := range events {
		if e.Kind != EventError || e.Err == nil {
			continue
		}
		found = true
		if strings.Contains(e.Err.Error(), key) {
			t.Errorf("a failure quotes the credential: %q", e.Err)
		}
	}
	if !found {
		t.Error("no failure reached the reader, want one")
	}
}

// TestStreamHoldsAStallForRetry drives the parser directly, so the reported
// failure is covered without a network and without a credential.
//
// The failure must not reach the callback here. Nothing was delivered, so the
// request has not failed and the attempt may be made again, and telling the
// reader it had would report a broken reply for one that is about to arrive.
func TestStreamHoldsAStallForRetry(t *testing.T) {
	c := &Client{apiKey: "k"}

	var got []Event
	err := c.stream(t.Context(),
		strings.NewReader(sse(stallPayload("Upstream idle timeout exceeded"))),
		func(e Event) { got = append(got, e) })

	var undelivered *undeliveredError
	if !errors.As(err, &undelivered) {
		t.Fatalf("stream returned %v, want a failure with nothing delivered", err)
	}
	if !strings.Contains(undelivered.msg, "idle timeout") {
		t.Errorf("failure is %q, want the provider's message", undelivered.msg)
	}
	for _, e := range got {
		if e.Kind == EventError {
			t.Errorf("a retryable failure reached the reader: %v", e.Err)
		}
	}
}

// TestPauseEndsOnAStoppedTurn is the read side of the retry: a reader who stops a
// turn during the backoff must not be made to wait it out.
//
// The pause is taken in steps so a cancelled context is noticed, which is the
// property this test would fail on if pause slept through its whole length.
func TestPauseEndsOnAStoppedTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := pause(ctx, retryWait*4); !errors.Is(err, context.Canceled) {
		t.Errorf("pause returned %v, want context.Canceled", err)
	}
}

// TestRetryWaitsDouble covers the backoff itself, which is otherwise only
// observable as a test taking its time.
func TestRetryWaitsDouble(t *testing.T) {
	waits := retryWaits(retryBound)
	if len(waits) != retryBound-1 {
		t.Fatalf("got %d waits, want %d: one pause between attempts, not one per attempt",
			len(waits), retryBound-1)
	}
	for i := 1; i < len(waits); i++ {
		if waits[i] <= waits[i-1] {
			t.Errorf("wait %d is %v, want more than wait %d at %v",
				i, waits[i], i-1, waits[i-1])
		}
	}
	if waits[0] <= 0 {
		t.Errorf("the first wait is %v, want a pause before the first retry", waits[0])
	}
}

// TestAttemptBoundAllowsARetry guards the constant itself, since a bound of one
// turns the retry into the single attempt it replaced.
func TestAttemptBoundAllowsARetry(t *testing.T) {
	if retryBound < 2 {
		t.Errorf("retryBound is %d, want at least 2", retryBound)
	}
}
