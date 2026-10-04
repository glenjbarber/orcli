// Package cloudflare is the client for the Cloudflare API.
//
// It is a leaf beside internal/openrouter and imports nothing from this module.
// internal/tui reaches it to serve the /cloudflare command, which is the one new
// edge in the tree and is deliberate: the command is in the interface, so the
// client is reached from there rather than from main.
//
// The credential is read from the configuration file and is never read from the
// environment. It is filtered out of every string that reaches a reader, since a
// log row is a thing a reader selects and pastes somewhere else.
package cloudflare

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// BaseURL is the API root. It is a variable rather than a constant so a test can
// point the client at a test server, and a test is the only reason a base URL
// would ever differ.
var BaseURL = "https://api.cloudflare.com/client/v4"

// ErrNoCredential is the absence that stops this package working.
//
// The Cloudflare key is not the credential the program itself runs on, so it is
// not fatal to a session that never types /cloudflare. It is fatal to a reader
// who did, since there is nothing to make the call with.
var ErrNoCredential = errors.New("cloudflare: no API key, put one under \"cloudflare\" in the configuration file")

// ErrNoZone is returned when a DNS call needs a zone and none was named.
//
// Cloudflare resolves a record through its zone, so a reader who named a record
// and no zone has named half of what the call needs rather than all of it.
var ErrNoZone = errors.New("cloudflare: no zone, name one with --zone")

// ErrUnknownSub is returned for a sub-command this package does not serve.
//
// It is named rather than generic, so a reader who typed a sub-command this
// client does not have is told which one rather than being told their arguments
// were wrong.
var ErrUnknownSub = errors.New("cloudflare: unknown sub-command")

// ErrNoResponse is returned when the transport produced neither a response nor an
// error, which no http.Client should do and which would otherwise be a nil
// dereference rather than a report.
var ErrNoResponse = errors.New("cloudflare: the transport returned no response and no error")

// APIClient is the HTTP client, narrowed to what this package uses.
type APIClient struct {
	// Base is the API root this client reads.
	Base string

	// HTTP is the transport. It is nil for the default, so a caller that wants
	// the default does not have to name one.
	HTTP *http.Client

	// Credential is the API key, filtered out of anything bound for a reader.
	Credential string
}

// New returns a client for the given key.
//
// The key is not validated here. A reader who has a key with a typo in it should
// be told by the endpoint, which names the fault, rather than refused here by a
// check that guesses at the shape.
func New(key string) *APIClient {
	return &APIClient{
		Base:       BaseURL,
		HTTP:       &http.Client{},
		Credential: key,
	}
}

// Filter removes the credential from s.
//
// It is applied at the boundary where a string becomes something a reader sees,
// not at the point the string was built, since a filter placed at the point of
// building is one a later call site forgets. A string that does not carry the
// credential comes back unchanged, which is the common case and must cost the
// least.
func (c *APIClient) Filter(s string) string {
	if c == nil || c.Credential == "" {
		return s
	}
	return strings.ReplaceAll(s, c.Credential, "[redacted]")
}

// FilterErr returns err with the credential removed from its text.
//
// It returns an error rather than a string so the caller can keep wrapping it
// with %w, which a filtered string cannot be. An error that does not carry the
// credential comes back as itself, so errors.Is still reaches the cause.
func (c *APIClient) FilterErr(err error) error {
	if err == nil || c == nil || c.Credential == "" {
		return err
	}
	filtered := c.Filter(err.Error())
	if filtered == err.Error() {
		return err
	}
	return errors.New(filtered)
}

// transport returns the transport this client uses.
func (c *APIClient) transport() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// do makes one request and decodes the body into out.
//
// Every outcome is a result: a status the endpoint refused, a body that did not
// decode, and a body that decoded are all values the caller reports, and none of
// them produces nothing. The credential is filtered from an error before it is
// returned, since an endpoint that quoted the request back has put the key in a
// string a reader will see.
func (c *APIClient) do(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cloudflare: %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, c.Base+path, reader)
	if err != nil {
		return fmt.Errorf("cloudflare: %s %s: %w", method, path, c.FilterErr(err))
	}
	req.Header.Set("Authorization", "Bearer "+c.Credential)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.transport().Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare: %s %s: %w", method, path, c.FilterErr(err))
	}
	if resp == nil {
		return fmt.Errorf("cloudflare: %s %s: %w", method, path, ErrNoResponse)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cloudflare: %s %s: %w", method, path, c.FilterErr(err))
	}
	// The body is filtered before anything reads it, so a credential the endpoint
	// echoed cannot survive into a decoded value either.
	text := c.Filter(string(raw))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("cloudflare: %s %s: %w", method, path, c.statusError(resp.StatusCode, text))
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("cloudflare: %s %s: %w", method, path, err)
	}
	return nil
}

// apiMessage is one message the endpoint gave, in either of the two shapes
// Cloudflare reports under.
type apiMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// statusError is a refusal by the endpoint, carrying the body it refused with.
//
// The body is kept rather than discarded since Cloudflare names the fault in it,
// and a reader told "400" and nothing else is a reader who has to go and find out
// why.
type statusError struct {
	Code   int
	Body   string
	Reason string
}

// Error is the refusal as a reader sees it. It carries the reason and the status
// and not the body, since the body is in Body for a caller that wants it and a
// row in a log does not need a JSON document in it.
func (e *statusError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("cloudflare: %d", e.Code)
	}
	return fmt.Sprintf("cloudflare: %d: %s", e.Code, e.Reason)
}

// statusError builds the error for a refused request. The body it is given has
// already been filtered.
func (c *APIClient) statusError(code int, text string) error {
	var payload struct {
		Errors   []apiMessage `json:"errors"`
		Messages []apiMessage `json:"messages"`
	}

	err := &statusError{Code: code, Body: text}
	if json.Unmarshal([]byte(text), &payload) == nil {
		err.Reason = firstMessage(payload.Errors, payload.Messages)
	}
	return err
}

// firstMessage returns the first message the endpoint gave, in either shape.
//
// Cloudflare reports errors under "errors" and some routes under "messages", and
// a reader who got the wrong one is told nothing at all.
func firstMessage(lists ...[]apiMessage) string {
	for _, list := range lists {
		for _, m := range list {
			if m.Message != "" {
				return m.Message
			}
		}
	}
	return ""
}
