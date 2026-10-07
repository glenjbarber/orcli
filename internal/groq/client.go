package groq

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

	"github.com/glenjbarber/orcli/internal/openrouter"
)

// ErrNoAPIKey is returned when a request is attempted with no credential.
//
// It is caught locally, before the request, on the same grounds
// [openrouter.ErrNoAPIKey] is: the backend answers a request with no
// credential exactly as it answers an invalid one, and a caller cannot tell
// those two apart from the response alone.
var ErrNoAPIKey = errors.New("groq: no API key configured")

// retryBound and retryWait mirror internal/openrouter's own constants of the
// same name. They are not shared across the two packages: each is a transport
// for a different endpoint, and a constant one package exported for the other
// to import would be the first import edge between two packages that AGENTS.md
// otherwise keeps independent of one another. Keeping the figure in both
// places costs six lines; importing one package's constant into another costs
// a dependency neither transport otherwise needs.
const retryBound = 6

// retryWait is the first pause between attempts. Each pause after it is twice
// the one before, the same backoff [openrouter.retryWaits] computes.
const retryWait = 250 * time.Millisecond

// undeliveredError is a failure the endpoint reported for an attempt that
// delivered nothing. It is what [Client.Chat] decides a retry on, mirroring
// internal/openrouter's own undeliveredError.
type undeliveredError struct {
	msg string
}

// Error implements error.
func (e *undeliveredError) Error() string { return "groq: " + e.msg }

// Client talks to the Groq API.
//
// The zero value is not usable. A Client is built by [New] and carries a base
// URL and a credential, neither of which has a defensible default.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New returns a Client carrying the given credential.
//
// The base URL is fixed at Groq's own OpenAI-compatible host. A caller that
// cannot reach it is a network fault and is reported as one, rather than
// being retried against another host.
func New(apiKey string) *Client {
	return &Client{
		baseURL: "https://api.groq.com/openai/v1",
		apiKey:  apiKey,
		http:    &http.Client{},
	}
}

// Chat sends a conversation and streams the reply.
//
// This is internal/openrouter's own [openrouter.Client.Chat], read against
// Groq's host instead of OpenRouter's: the same retry-on-undelivered rule,
// the same callback-not-return-value failure reporting, and the same
// required [DONE] terminator. See that method's doc comment for why each of
// those exists; nothing here departs from it, because nothing about streaming
// an OpenAI-compatible chat completion differs between the two endpoints.
//
// One field of [openrouter.Request] is OpenRouter's own: AttributionID is
// sent as the endpoint's `HTTP-Referer`-equivalent attribution and Groq's API
// has no matching concept, so this client simply never populates it from the
// request - an AttributionID a caller set is silently not sent, which is the
// right outcome for a caller using the one Request type both transports
// share rather than a refusal over a field this endpoint has no name for.
func (c *Client) Chat(ctx context.Context, req openrouter.Request, onEvent func(openrouter.Event)) error {
	if onEvent == nil {
		return errors.New("groq: Chat requires an event callback")
	}
	if c.apiKey == "" {
		return ErrNoAPIKey
	}

	waits := retryWaits(retryBound)

	var last *undeliveredError
	for attempt := range retryBound {
		if attempt > 0 {
			if err := pause(ctx, waits[attempt-1]); err != nil {
				onEvent(openrouter.Event{Kind: openrouter.EventError, Err: err})
				return nil
			}
		}

		resp, err := c.post(ctx, req)
		if err != nil {
			onEvent(openrouter.Event{Kind: openrouter.EventError, Err: err})
			return nil
		}

		if resp.StatusCode != http.StatusOK {
			err := boundedError(c, resp)
			resp.Body.Close()
			onEvent(openrouter.Event{Kind: openrouter.EventError, Err: err})
			return nil
		}

		err = c.stream(ctx, resp.Body, onEvent)
		resp.Body.Close()

		switch {
		case err == nil:
			return nil
		case ctx.Err() != nil:
			return err
		case errors.As(err, &last):
			continue
		default:
			return nil
		}
	}

	onEvent(openrouter.Event{Kind: openrouter.EventError, Err: last})
	onEvent(openrouter.Event{Kind: openrouter.EventFinish, Finished: false})
	return nil
}

// retryWaits returns the pause preceding each attempt after the first, the
// same derivation [openrouter.retryWaits] uses so a bound and a backoff here
// cannot disagree either.
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

// get makes one GET request against path and decodes a successful body into
// out, mirroring [openrouter.Client.get].
func (c *Client) get(ctx context.Context, path string, out any) error {
	if c.apiKey == "" {
		return ErrNoAPIKey
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("groq: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("groq: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return boundedError(c, resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("groq: decode %s: %w", path, err)
	}
	return nil
}

// Filter redacts the credential from a string bound for a diagnostic.
//
// It delegates to [openrouter.Filter] rather than reimplementing the same
// substring replacement: redacting a secret from a string a caller is about
// to show on a screen is not a thing specific to either provider, and the two
// clients sharing the one implementation is a dependency neither client
// minds carrying, unlike the request and reply types [New]'s own package doc
// explains this package reuses for a different reason.
func Filter(s, secret string) string {
	return openrouter.Filter(s, secret)
}

// boundedError reads an error body under a limit and quotes what it read.
func boundedError(c *Client, resp *http.Response) error {
	const limit = 512
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return fmt.Errorf("groq: HTTP %s", resp.Status)
	}
	msg := strings.TrimSpace(Filter(string(b), c.apiKey))
	if msg == "" {
		return fmt.Errorf("groq: HTTP %s", resp.Status)
	}
	return fmt.Errorf("groq: HTTP %s: %s", resp.Status, msg)
}

// post sends the request and returns the response without reading its body.
func (c *Client) post(ctx context.Context, req openrouter.Request) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("groq: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("groq: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("groq: %w", err)
	}
	return resp, nil
}
