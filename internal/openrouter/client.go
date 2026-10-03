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
)

// ErrNoAPIKey is returned when a request is attempted with no credential.
//
// It is caught locally, before the request. The backend answers a request with
// no credential exactly as it answers an invalid one, and a caller cannot tell
// those two apart from the response alone.
var ErrNoAPIKey = errors.New("openrouter: no API key configured")

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
func (c *Client) Chat(ctx context.Context, req Request, onEvent func(Event)) error {
	if onEvent == nil {
		return errors.New("openrouter: Chat requires an event callback")
	}
	if c.apiKey == "" {
		return ErrNoAPIKey
	}

	// The status is examined without reading the body on success, because the
	// body is the stream. Reading it would consume the reply before the parser
	// seen it. On an error path the body is read, bounded, and quoted.
	resp, err := c.post(ctx, req)
	if err != nil {
		onEvent(Event{Kind: EventError, Err: err})
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		onEvent(Event{Kind: EventError, Err: boundedError(c, resp)})
		return nil
	}

	return c.stream(ctx, resp.Body, onEvent)
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
