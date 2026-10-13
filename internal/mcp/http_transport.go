package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// mcpSessionHeader is the header a Streamable HTTP server assigns a session
// ID under in its initialize response, and the header every later request in
// the same session carries it back under (MCP spec 2025-03-26).
const mcpSessionHeader = "Mcp-Session-Id"

// httpTransport is one remote MCP server reached over the Streamable HTTP
// transport: every JSON-RPC message is POSTed, as its own HTTP request, to
// one configured URL.
//
// What is implemented: POSTing a request and reading back either a single
// `application/json` response body or a single-event `text/event-stream`
// response (a server may answer either way per the spec, and both are
// read here), session continuity via Mcp-Session-Id once a server assigns
// one, and best-effort session termination (DELETE) on Close.
//
// What is deliberately not implemented: a standalone server-initiated SSE
// stream opened with a bare GET to the same URL, used by a server to push a
// notification or request the client did not ask for. This client does not
// act on an unsolicited server message over stdio either (stdioTransport's
// own readLoop skips a line with no numeric id for the same reason), so
// there being no open GET stream to deliver one over is not a narrowing of
// behaviour this client already has - it is the same behaviour reached a
// second way. Resumable streams (Last-Event-ID) are not implemented either,
// since there is no standing stream to resume without that GET.
type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client

	mu        sync.Mutex
	sessionID string

	nextID int64
}

func newHTTPTransport(url string, headers map[string]string) *httpTransport {
	return &httpTransport{url: url, headers: headers, client: &http.Client{Timeout: callTimeout}}
}

// session returns the session ID this transport has been assigned, if any.
func (t *httpTransport) session() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

// setSession records a session ID a response assigned. A server is not
// required to assign one (the spec allows a sessionless server), so an empty
// value from a response changes nothing - it is never used to clear a
// session this transport already has.
func (t *httpTransport) setSession(id string) {
	if id == "" {
		return
	}
	t.mu.Lock()
	t.sessionID = id
	t.mu.Unlock()
}

// post sends one JSON-RPC message (request or notification) and returns the
// raw HTTP response, with every header and the session ID this transport
// has on hand attached.
func (t *httpTransport) post(body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if id := t.session(); id != "" {
		req.Header.Set(mcpSessionHeader, id)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: %w", t.url, err)
	}
	t.setSession(resp.Header.Get(mcpSessionHeader))
	return resp, nil
}

// call sends a request and returns its result, or an error naming what went
// wrong - a non-2xx status, a body that is neither JSON nor a parseable
// single-event SSE stream, or a JSON-RPC error object in the response.
func (t *httpTransport) call(method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&t.nextID, 1)
	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: encode %s: %w", method, err)
	}

	resp, err := t.post(data)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("mcp: %s: %s returned %s: %s", method, t.url, resp.Status, strings.TrimSpace(string(detail)))
	}

	rpcResp, err := decodeRPCResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: %w", method, err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp: %s: %s (code %d)", method, rpcResp.Error.Message, rpcResp.Error.Code)
	}
	return rpcResp.Result, nil
}

// notify sends a JSON-RPC message with no reply expected. Per the spec a
// server answers a notification-only POST with 202 Accepted and an empty
// body; any 2xx is accepted here without trying to parse a body that may not
// exist.
func (t *httpTransport) notify(method string, params any) error {
	note := rpcNotification{JSONRPC: "2.0", Method: method, Params: params}
	data, err := json.Marshal(note)
	if err != nil {
		return fmt.Errorf("mcp: encode %s: %w", method, err)
	}

	resp, err := t.post(data)
	if err != nil {
		return fmt.Errorf("mcp: %s: %w", method, err)
	}
	defer io.Copy(io.Discard, resp.Body)
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mcp: %s: %s returned %s", method, t.url, resp.Status)
	}
	return nil
}

// decodeRPCResponse reads a POST response body as either a bare JSON object
// or a single-event text/event-stream body, whichever Content-Type the
// server actually sent - the spec permits either for a request that expects
// a reply.
func decodeRPCResponse(resp *http.Response) (rpcResponse, error) {
	contentType := resp.Header.Get("Content-Type")
	switch {
	case strings.Contains(contentType, "text/event-stream"):
		return decodeSSEResponse(resp.Body)
	default:
		// application/json is the expected case; anything else is still
		// handed to the JSON decoder rather than refused up front, since a
		// server that omitted or mislabelled the header but sent a legal
		// JSON body should still work rather than being refused on a
		// technicality this client can see past.
		var out rpcResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return rpcResponse{}, fmt.Errorf("decode response body (content-type %q): %w", contentType, err)
		}
		return out, nil
	}
}

// decodeSSEResponse reads one Server-Sent Events stream until it finds a
// `data:` field, parses that as the JSON-RPC response, and stops - a
// Streamable HTTP POST response is a single reply framed as one SSE event,
// not an open-ended stream of many, so reading past the first complete event
// is reading past what this request's own response ever contains.
//
// An SSE event can carry its data split across multiple `data:` lines
// (joined with newlines per the SSE spec) and may carry `event:`/`id:`
// fields this client does not need and skips. The event ends at the first
// blank line, which is this function's own stopping point.
func decodeSSEResponse(r io.Reader) (rpcResponse, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)

	var data strings.Builder
	sawData := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if sawData {
				break
			}
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		if field != "data" {
			continue
		}
		if sawData {
			data.WriteByte('\n')
		}
		data.WriteString(strings.TrimPrefix(value, " "))
		sawData = true
	}
	if err := scanner.Err(); err != nil {
		return rpcResponse{}, fmt.Errorf("read event stream: %w", err)
	}
	if !sawData {
		return rpcResponse{}, fmt.Errorf("event stream carried no data field")
	}

	var out rpcResponse
	if err := json.Unmarshal([]byte(data.String()), &out); err != nil {
		return rpcResponse{}, fmt.Errorf("decode event stream data: %w", err)
	}
	return out, nil
}

// Close ends this transport's session, if the server assigned one. Per the
// spec a client SHOULD send DELETE to explicitly terminate a session; this
// is best-effort, since there is nothing useful to do locally if a remote
// server does not acknowledge it, and failing to tear down a remote session
// is not a reason to fail the local Close call the rest of this package
// depends on succeeding.
func (t *httpTransport) Close() error {
	id := t.session()
	if id == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodDelete, t.url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set(mcpSessionHeader, id)
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}
