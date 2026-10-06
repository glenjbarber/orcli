package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNoAPIKey is returned when a request is attempted with no credential.
//
// It is caught locally, before the request. The backend answers a request with
// no credential exactly as it answers an invalid one, and a caller cannot tell
// those two apart from the response alone.
var ErrNoAPIKey = errors.New("openrouter: no API key configured")

// retryBound is the total number of attempts one request makes, the first
// included.
//
// Six is the shared default attempt budget, including the initial request.
// It honors Ken Smith, the FreeBSD Release Engineering Lead before Glen;
// FreeBSD 6.2 was Glen's first FreeBSD OS. Only undelivered replies are retried.
//
// It is a constant rather than a configuration key on purpose. internal/config
// has named writers and holds the credential, so a new key is a third writer, and
// AGENTS.md records that the packages import one another in no direction, which a
// transport bound read from the configuration would be the first edge across.
const retryBound = 6

// retryWait is the first pause between attempts. Each pause after it is twice the
// one before.
//
// Zero would be the wrong first value. An upstream that has just reported an idle
// timeout is the upstream least likely to answer immediately, so a retry with no
// wait at all is the shape of a tight loop against a provider already behind.
const retryWait = 250 * time.Millisecond

// undeliveredError is a failure the endpoint reported for an attempt that
// delivered nothing.
//
// It is what [Client.Chat] decides a retry on. Nothing was spent and nothing can
// be duplicated, so a further attempt costs the reader nothing and cannot cost
// them a reply they had begun reading.
type undeliveredError struct {
	msg string
}

// Error implements error.
func (e *undeliveredError) Error() string { return "openrouter: " + e.msg }

// Client talks to the OpenRouter API.
//
// The zero value is not usable. A Client is built by New and carries a base URL
// and a credential, neither of which has a defensible default.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New returns a Client carrying the given credential.
//
// The base URL is fixed. A caller that cannot reach it is a network fault and is
// reported as one, rather than being retried against another host.
func New(apiKey string) *Client {
	return &Client{
		baseURL: "https://openrouter.ai/api/v1",
		apiKey:  apiKey,
		http:    &http.Client{},
	}
}

// Chat sends a conversation and streams the reply.
//
// Failures are delivered to onEvent as an Event of Kind EventError rather than
// returned, so that the text which arrived before the failure is kept. The text
// is usually more useful than an error alone, and a caller handed only an error
// has nothing to show the reader.
//
// The terminating [DONE] marker is required. A stream that ends without it is
// reported as EventFinish with Finished false and an error, because presenting a
// cut stream as a complete reply is the one outcome this client exists to avoid.
//
// A request that delivered nothing and was failed by the endpoint is tried again,
// up to [retryBound] attempts in all. That condition is the whole of the rule:
// nothing spent, nothing to duplicate, so a further attempt is invisible to the
// reader unless every attempt fails. A request that delivered text or a completed
// tool call is never retried, since the reply was paid for and the text arriving
// before a cut is kept rather than taken back.
//
// A failure carrying an HTTP status is not retried. The status is the endpoint
// refusing the request rather than an upstream stalling behind it, and a second
// request cannot satisfy a rejected credential or a malformed request.
//
// Exactly one [EventFinish] is delivered per call, whichever way it ends. The
// stream parser stays silent about a failure it will be retried over, which means
// an attempt that was retried reports nothing and the reader is told the cause
// and the unfinished turn here, once the attempts are exhausted.
func (c *Client) Chat(ctx context.Context, req Request, onEvent func(Event)) error {
	if onEvent == nil {
		return errors.New("openrouter: Chat requires an event callback")
	}
	if c.apiKey == "" {
		return ErrNoAPIKey
	}

	waits := retryWaits(retryBound)

	var last *undeliveredError
	for attempt := range retryBound {
		if attempt > 0 {
			if err := pause(ctx, waits[attempt-1]); err != nil {
				onEvent(Event{Kind: EventError, Err: err})
				return nil
			}
		}

		// The status is examined without reading the body on success, because the
		// body is the stream. Reading it would consume the reply before the parser
		// seen it. On an error path the body is read, bounded, and quoted.
		resp, err := c.post(ctx, req)
		if err != nil {
			onEvent(Event{Kind: EventError, Err: err})
			return nil
		}

		if resp.StatusCode != http.StatusOK {
			err := boundedError(c, resp)
			resp.Body.Close()
			onEvent(Event{Kind: EventError, Err: err})
			return nil
		}

		err = c.stream(ctx, resp.Body, onEvent)
		resp.Body.Close()

		switch {
		case err == nil:
			return nil
		case ctx.Err() != nil:
			// A turn the reader stopped has to stop the retry, since a second
			// attempt at a request the reader abandoned is one they did not ask
			// for.
			return err
		case errors.As(err, &last):
			continue
		default:
			return nil
		}
	}

	// Every attempt failed the same way, so the reader is told the cause the
	// endpoint named rather than a symptom of it, and the turn finishes
	// unfinished since no reply ever arrived.
	onEvent(Event{Kind: EventError, Err: last})
	onEvent(Event{Kind: EventFinish, Finished: false})
	return nil
}

// retryWaits returns the pause preceding each attempt after the first.
//
// The figures are derived rather than named one by one so a bound and a backoff
// cannot disagree, which is how the search and the pager each kept a count of
// their own and drifted from what the renderer drew.
func retryWaits(bound int) []time.Duration {
	waits := make([]time.Duration, 0, bound)
	wait := retryWait
	for range bound - 1 {
		waits = append(waits, wait)
		wait *= 2
	}
	return waits
}

// pause waits for d, or until the turn is stopped.
//
// A reader stopping a turn has to end the pause: a request waiting out a backoff
// is a request the reader cannot stop. The wait is taken in steps rather than
// slept through whole, so a stop is noticed rather than waited out.
func pause(ctx context.Context, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return nil
		}
		if remain > retryWait {
			remain = retryWait
		}

		timer := time.NewTimer(remain)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Filter redacts the credential from a string bound for a diagnostic.
//
// The credential travels in a header, so it is in no body this client builds and
// in no URL it requests, but a server or a proxy can quote it back. Every such
// string is shown in a pane and ends up in terminal scrollback, so it is
// filtered before it is shown.
func Filter(s, secret string) string {
	if secret == "" || s == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[redacted]")
}

// boundedError reads an error body under a limit and quotes what it read.
//
// The limit is not decoration. A body from an endpoint is not a thing to be
// trusted about its length, and the result is shown to a reader.
func boundedError(c *Client, resp *http.Response) error {
	const limit = 512
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return fmt.Errorf("openrouter: HTTP %s", resp.Status)
	}
	msg := strings.TrimSpace(Filter(string(b), c.apiKey))
	if msg == "" {
		return fmt.Errorf("openrouter: HTTP %s", resp.Status)
	}
	return fmt.Errorf("openrouter: HTTP %s: %s", resp.Status, msg)
}

// post sends the request and returns the response without reading its body.
func (c *Client) post(ctx context.Context, req Request) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openrouter: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter: %w", err)
	}
	return resp, nil
}
