// Package mcp provides model-callable tools backed by Model Context Protocol
// servers the reader has configured. A server is reached one of two ways: a
// local command launched as a subprocess and spoken to over its standard
// input and output, or a remote server reached over HTTP using the
// Streamable HTTP transport (MCP spec 2025-03-26) - the transport the
// specification currently recommends for new integrations. The older
// HTTP+SSE transport is legacy and is not implemented; a server that only
// offers it is not reachable from this client.
//
// Only resource listing and resource reading are offered today. A server's
// tools and prompts are not surfaced.
//
// # Transport
//
// Both transports speak the same JSON-RPC 2.0 request/notification/response
// shape; only how a message physically travels differs, which is why
// client's own protocol methods (initialize, listResources, readResource)
// are written once against the transport interface below and never need to
// know which kind of server they are talking to.
//
// The stdio transport is one message per line on stdin/stdout, matching the
// protocol's stdio transport. A subprocess is given PATH plus whatever the
// reader named under the server's own env block in the configuration file,
// and nothing else: the same rule internal/tools states for the shell and
// git tools, so a server this package starts cannot inherit a credential the
// reader did not explicitly hand it.
//
// The HTTP transport POSTs one JSON-RPC message per call to the server's
// configured URL and reads back either a single `application/json` response
// body or a single-event `text/event-stream` response, whichever the server
// sends (see http_transport.go's own doc comment for what is and is not
// implemented - notably, a standalone server-initiated SSE stream opened
// with GET is not, since this client does not act on unsolicited server
// notifications over stdio either).
//
// # Lazy, and kept once started
//
// A server named in the configuration file is not started until the model
// actually calls a tool naming it. A session that never asks a configured
// server for anything never pays the cost of starting it. Once started, a
// server is kept and reused by later calls in the same session rather than
// relaunched or re-dialed each time, since the handshake a server requires
// before its first real request is not free either.
package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/glenjbarber/orcli/internal/tools"
)

// callTimeout bounds how long a single JSON-RPC round trip may take before it
// is reported as timed out rather than left to hang the turn that asked for
// it. A local subprocess or a remote server that has not answered within this
// window is treated as broken, and the next call to the same server starts a
// fresh one.
//
// It is a variable, not a constant, only so a test can shrink it rather than
// run for the full 30 seconds to prove a timeout is reported; production
// code never writes to it.
var callTimeout = 30 * time.Second

// protocolVersion is the MCP protocol version this client speaks during the
// initialize handshake.
const protocolVersion = "2024-11-05"

// maxResourcePages bounds how many pages of resources/list this client will
// follow for one call, so a server whose cursor never ends empty cannot turn
// one tool call into an unbounded loop.
const maxResourcePages = 64

// Server names one MCP server the reader has configured.
//
// A local server names Command (and, optionally, Args/Env); a remote one
// names URL (and, optionally, Headers) instead. The two are mutually
// exclusive - internal/config.MCPServers refuses a server naming both or
// neither before this package ever sees it, so start treats a non-empty URL
// as the only signal it needs to pick the HTTP transport.
type Server struct {
	// Name is how the model names this server when it calls a tool. It is
	// the key the reader chose in the configuration file, not something
	// this package invents.
	Name string

	// Command is the program to run, resolved by the search path the way
	// internal/tools resolves the shell tool's programs. Empty for a
	// remote server.
	Command string

	// Args are the arguments the program is started with. Local only.
	Args []string

	// Env is the subprocess's environment, as KEY=VALUE pairs, in addition
	// to PATH. It is never read from this process's own environment: a
	// server that needs a credential gets it because the reader wrote it
	// into the server's own env block, the same way every other credential
	// in the configuration file is handled. Local only.
	Env []string

	// URL is the remote server's own Streamable HTTP endpoint - one URL
	// every JSON-RPC message is POSTed to. Empty for a local server.
	URL string

	// Headers are sent with every request to a remote server (for example
	// Authorization), the HTTP transport's own equivalent of Env: a
	// credential a remote server needs is here because the reader wrote it
	// here, never read from this process's own environment. Remote only.
	Headers map[string]string
}

// transport is the seam client's protocol methods are written against, so
// initialize/listResources/readResource never need to know whether they are
// talking to a subprocess or a remote endpoint.
type transport interface {
	call(method string, params any) (json.RawMessage, error)
	notify(method string, params any) error
	Close() error
}

// Manager holds the MCP servers a session was configured with, and starts
// each one's subprocess or opens its HTTP session lazily, on the first call
// that names it.
type Manager struct {
	mu      sync.Mutex
	servers map[string]Server
	clients map[string]*client
}

// NewManager returns a Manager over the given servers. A nil or empty list is
// a Manager with nothing to offer: its ToolSet still returns the two tools,
// and each refuses with the (empty) list of configured servers named in the
// refusal.
func NewManager(servers []Server) *Manager {
	m := &Manager{servers: make(map[string]Server, len(servers)), clients: make(map[string]*client)}
	for _, s := range servers {
		m.servers[s.Name] = s
	}
	return m
}

// Close stops every server this Manager has started. A server never called
// by the model was never started and has nothing to stop.
func (m *Manager) Close() error {
	m.mu.Lock()
	clients := m.clients
	m.clients = make(map[string]*client)
	m.mu.Unlock()

	for _, c := range clients {
		c.Close()
	}
	return nil
}

// ToolSet returns the two tools this package offers.
func (m *Manager) ToolSet() []tools.Tool {
	return []tools.Tool{
		&listResourcesTool{m: m},
		&readResourceTool{m: m},
	}
}

// names returns the configured server names, sorted, for a refusal that
// names what the model could have asked for instead.
func (m *Manager) names() []string {
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// configuredServers renders the configured names for a tool description, or
// says there are none.
func (m *Manager) configuredServers() string {
	names := m.names()
	if len(names) == 0 {
		return "no MCP server is configured"
	}
	return "configured servers: " + strings.Join(names, ", ")
}

// get returns the running client for a configured server, starting (local)
// or dialing (remote) and initializing it on first use.
func (m *Manager) get(name string) (*client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if c, ok := m.clients[name]; ok {
		return c, nil
	}

	server, ok := m.servers[name]
	if !ok {
		return nil, fmt.Errorf("mcp: no server named %q is configured; %s", name, m.configuredServers())
	}

	c, err := start(server)
	if err != nil {
		return nil, fmt.Errorf("mcp: start %s: %w", name, err)
	}
	if err := c.initialize(); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp: initialize %s: %w", name, err)
	}

	m.clients[name] = c
	return c, nil
}

// drop closes and discards a server's client, so the next call to it starts
// a fresh subprocess or HTTP session rather than reusing one a prior call
// found broken.
func (m *Manager) drop(name string) {
	m.mu.Lock()
	c, ok := m.clients[name]
	if ok {
		delete(m.clients, name)
	}
	m.mu.Unlock()

	if ok {
		c.Close()
	}
}

// rpcRequest is a JSON-RPC 2.0 call.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcNotification is a JSON-RPC 2.0 message with no reply expected.
type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is what a server sends back. ID is read as raw JSON because an
// MCP server's own notification to the client can arrive on the same stream
// with no id at all, and this client tells the two apart by whether ID
// decodes to a number rather than by a separate shape.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// client is one running MCP server - local or remote - spoken to through its
// own transport. Every protocol-level method below (initialize,
// listResources, readResource) is written once against transport and works
// unchanged for either kind of server.
type client struct {
	t transport
}

// start opens a server's transport - launching a subprocess for a local
// server, or preparing an HTTP session for a remote one - based on which of
// Command/URL the Server names. It does not perform the initialize
// handshake; the caller does that once the client is constructed, so a
// failed handshake can close what start opened.
func start(s Server) (*client, error) {
	if s.URL != "" {
		return &client{t: newHTTPTransport(s.URL, s.Headers)}, nil
	}
	t, err := newStdioTransport(s)
	if err != nil {
		return nil, err
	}
	return &client{t: t}, nil
}

// call sends a request and waits for its matching response, or for
// callTimeout to pass. Delegates to the transport; see stdioTransport.call
// and httpTransport.call for how each kind actually waits.
func (c *client) call(method string, params any) (json.RawMessage, error) {
	return c.t.call(method, params)
}

// notify sends a JSON-RPC message with no reply expected.
func (c *client) notify(method string, params any) error {
	return c.t.notify(method, params)
}

// Close stops the subprocess, or ends the HTTP session, depending on which
// transport this client was opened with.
func (c *client) Close() error {
	return c.t.Close()
}

// initialize performs the handshake every MCP server requires before any
// other request.
func (c *client) initialize() error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "orcli", "version": "0.0.0-dev"},
	}
	if _, err := c.call("initialize", params); err != nil {
		return err
	}
	// The initialized notification has no reply, and a server that never
	// receives it is a server the rest of this protocol has not agreed to
	// talk to yet.
	return c.notify("notifications/initialized", map[string]any{})
}

// Resource is one resource an MCP server named in a resources/list reply.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

// listResourcesResult is the reply to resources/list.
type listResourcesResult struct {
	Resources  []Resource `json:"resources"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// listResources returns every resource a server offers, following its cursor
// until the server reports there is no more.
func (c *client) listResources() ([]Resource, error) {
	var all []Resource
	cursor := ""

	for page := 0; page < maxResourcePages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call("resources/list", params)
		if err != nil {
			return nil, err
		}
		var result listResourcesResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("mcp: resources/list: decode: %w", err)
		}
		all = append(all, result.Resources...)
		if result.NextCursor == "" {
			return all, nil
		}
		cursor = result.NextCursor
	}
	return nil, fmt.Errorf("mcp: resources/list: did not end after %d pages", maxResourcePages)
}

// resourceContent is one entry of a resources/read reply.
type resourceContent struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// readResourceResult is the reply to resources/read.
type readResourceResult struct {
	Contents []resourceContent `json:"contents"`
}

// readResource returns a resource's contents as text.
//
// A text entry is returned as written. A binary entry is reported by its
// MIME type and size rather than decoded: this tool reads a resource as text
// for a model to read, the same promise internal/tools.readFileTool makes,
// and base64 bytes handed to a model as "content" would be read as
// something it is not.
func (c *client) readResource(uri string) (string, error) {
	raw, err := c.call("resources/read", map[string]any{"uri": uri})
	if err != nil {
		return "", err
	}
	var result readResourceResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("mcp: resources/read: decode: %w", err)
	}
	if len(result.Contents) == 0 {
		return "", fmt.Errorf("mcp: resources/read: %s returned no content", uri)
	}

	var b strings.Builder
	for i, content := range result.Contents {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		switch {
		case content.Text != "":
			b.WriteString(content.Text)
		case content.Blob != "":
			mimeType := content.MIMEType
			if mimeType == "" {
				mimeType = "unknown type"
			}
			fmt.Fprintf(&b, "[binary resource, %s, %d bytes of base64, not decoded]", mimeType, len(content.Blob))
		default:
			b.WriteString("[empty content]")
		}
	}
	return b.String(), nil
}
