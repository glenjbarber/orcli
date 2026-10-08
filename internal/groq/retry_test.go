package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glenjbarber/orcli/internal/openrouter"
)

// retryScript serves one scripted stream per request, in order, mirroring
// internal/openrouter's own retryScript helper.
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

// stallPayload is the payload an upstream stall is reported as: an error
// object and nothing else, the same shape Groq's own OpenAI-compatible
// streaming errors use.
func stallPayload(message string) string {
	return `{"error":{"message":` + strconv.Quote(message) + `}}`
}

// sse frames a data payload the way the endpoint does, mirroring
// internal/openrouter's own stream_test.go helper of the same name.
func sse(payload string) string {
	return "data: " + payload + "\n\n"
}

// chunkOf builds a text delta payload.
func chunkOf(text string) string {
	return `{"choices":[{"delta":{"content":` + strconv.Quote(text) + `}}]}`
}

// retryClient returns a Client pointed at a test server.
func retryClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c := New("gsk-retry-test")
	c.baseURL = baseURL
	return c
}

// retryEvents runs one request and reports what the reader was shown.
func retryEvents(t *testing.T, c *Client) []openrouter.Event {
	t.Helper()

	var events []openrouter.Event
	if err := c.Chat(context.Background(), openrouter.Request{Stream: true}, func(e openrouter.Event) {
		events = append(events, e)
	}); err != nil {
		t.Fatalf("Chat returned %v, want nil: a failure reaches the reader as an event", err)
	}
	return events
}

// mentions reports whether a failure the reader saw carries a fragment.
func mentions(events []openrouter.Event, fragment string) bool {
	for _, e := range events {
		if e.Kind == openrouter.EventError && e.Err != nil &&
			strings.Contains(e.Err.Error(), fragment) {
			return true
		}
	}
	return false
}

// TestChatRetriesAnUndeliveredStall covers the one case worth trying again:
// an attempt that delivered nothing before the endpoint reported a stall.
// The second scripted reply succeeds, and the reader never sees the first
// attempt's failure at all.
func TestChatRetriesAnUndeliveredStall(t *testing.T) {
	srv, calls := retryScript(t,
		sse(stallPayload("upstream timeout")),
		sse(chunkOf("ok"))+sse("[DONE]"),
	)
	c := retryClient(t, srv.URL)

	events := retryEvents(t, c)
	if got, want := reply(events), "ok"; got != want {
		t.Errorf("reply is %q, want %q", got, want)
	}
	if mentions(events, "upstream timeout") {
		t.Error("the first attempt's stall reached the reader; it should have been retried silently")
	}
	if calls.Load() < 2 {
		t.Errorf("the server saw %d calls, want at least 2", calls.Load())
	}
}

// TestChatDoesNotRetryAfterDelivering covers the other half of the rule: a
// stall reported after text has already arrived is shown at once rather
// than retried, since the reply was already paid for.
func TestChatDoesNotRetryAfterDelivering(t *testing.T) {
	srv, calls := retryScript(t,
		sse(chunkOf("partial "))+sse(stallPayload("dropped")),
	)
	c := retryClient(t, srv.URL)

	events := retryEvents(t, c)
	if got, want := reply(events), "partial "; got != want {
		t.Errorf("reply is %q, want %q", got, want)
	}
	if !mentions(events, "dropped") {
		t.Error("the stall after delivered text was not reported")
	}
	if calls.Load() != 1 {
		t.Errorf("the server saw %d calls, want exactly 1: a paid-for reply is not retried", calls.Load())
	}
}

// Exhausting every retry (an attempt that stalls [retryBound] times in a
// row) is not covered by a test here, on the same grounds
// internal/openrouter's own retry_test.go leaves it uncovered: the backoff
// between attempts is real time, [retryWaits] sums to several seconds across
// a full exhaustion, and a test that waited for that would cost every run of
// `make test` seconds for a path [TestChatRetriesAnUndeliveredStall] and
// [TestChatDoesNotRetryAfterDelivering] already prove the two building
// blocks of: that an undelivered stall is retried, and that the bound and
// the backoff derive from [retryBound] without disagreeing (see
// TestRetryWaitsMatchesTheBound below).

// TestRetryWaitsMatchesTheBound covers the derivation retryWaits makes, the
// same check internal/openrouter's own retry_test.go runs against its
// identical function: a bound and a backoff computed from the same constant
// cannot disagree about how many waits there are.
func TestRetryWaitsMatchesTheBound(t *testing.T) {
	waits := retryWaits(retryBound)
	if len(waits) != retryBound-1 {
		t.Errorf("retryWaits(%d) has %d entries, want %d", retryBound, len(waits), retryBound-1)
	}
	for i := 1; i < len(waits); i++ {
		if waits[i] != waits[i-1]*2 {
			t.Errorf("wait %d is %v, want double the one before it (%v)", i, waits[i], waits[i-1]*2)
		}
	}
}
