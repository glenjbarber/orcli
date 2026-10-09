// Package mcp provides model-callable tools backed by Model Context Protocol
// servers the reader has configured, each launched as a subprocess and spoken
// to over its standard input and output.
//
// Only resource listing and resource reading are offered today. A server's
// tools and prompts are not surfaced, and a remote server reached over HTTP
// or SSE rather than launched as a local subprocess is not supported; both
// are tracked as follow-ups rather than built here.
//
// # Transport
//
// The wire format is JSON-RPC 2.0, one message per line, matching the
// protocol's stdio transport. A subprocess is given PATH plus whatever the
// reader named under the server's own env block in the configuration file,
// and nothing else: the same rule internal/tools states for the shell and
// git tools, so a server this package starts cannot inherit a credential the
// reader did not explicitly hand it.
//
// # Lazy, and kept once started
//
// A server named in the configuration file is not started until the model
// actually calls a tool naming it. A session that never asks a configured
// server for anything never pays the cost of a subprocess for it. Once
// started, a server is kept running and reused by later calls in the same
// session rather than relaunched each time, since the handshake a server
// requires before its first real request is not free either.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/glenjbarber/orcli/internal/tools"
)

// callTimeout bounds how long a single JSON-RPC round trip may take before it
// is reported as timed out rather than left to hang the turn that asked for
// it. A local subprocess that has not answered within this window is treated
// as broken, and the next call to the same server starts a fresh one.
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

// Server names one MCP server the reader has configured, by the command and
// arguments that launch it.
type Server struct {
	// Name is how the model names this server when it calls a tool. It is
	// the key the reader chose in the configuration file, not something
	// this package invents.
	Name string

	// Command is the program to run, resolved by the search path the way
	// internal/tools resolves the shell tool's programs.
	Command string

	// Args are the arguments the program is started with.
	Args []string

	// Env is the subprocess's environment, as KEY=VALUE pairs, in addition
	// to PATH. It is never read from this process's own environment: a
	// server that needs a credential gets it because the reader wrote it
	// into the server's own env block, the same way every other credential
	// in the configuration file is handled.
	Env []string
}

// Manager holds the MCP servers a session was configured with, and starts
// each one's subprocess lazily, on the first call that names it.
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

// get returns the running client for a configured server, starting and
// initializing it on first use.
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
// a fresh subprocess rather than reusing one a prior call found broken.
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
// MCP server's own notification to the client arrives on the same stream with
// no id at all, and this client tells the two apart by whether ID decodes to
// a number rather than by a separate shape.
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

// client is one running MCP server, spoken to over newline-delimited
// JSON-RPC on its stdin and stdout.
//
// A single background goroutine owns the read side and demultiplexes
// responses to the call that is waiting for each one by ID, which is what
// lets a call time out without corrupting a read another call still needs:
// the reader never stops, it only stops being waited on.
type client struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResponse
	readErr error
}

// start launches a server's subprocess and begins reading its output. It
// does not perform the initialize handshake; the caller does that once the
// client is constructed, so a failed handshake can close what start opened.
func start(s Server) (*client, error) {
	cmd := exec.Command(s.Command, s.Args...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, s.Env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: open stdout: %w", err)
	}
	// Stderr is discarded rather than captured: a server's diagnostic chatter
	// is not a resource and is not shown to the model, and capturing it
	// without a bound would be one more place a hung or chatty server could
	// grow without limit.
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %s: %w", s.Command, err)
	}

	c := &client{cmd: cmd, stdin: stdin, pending: make(map[int64]chan rpcResponse)}
	go c.readLoop(stdout)
	return c, nil
}

// readLoop reads one JSON-RPC message per line for the life of the
// subprocess, and delivers each to whichever call is waiting for its ID. A
// line this client cannot parse, or one carrying no numeric ID - a
// notification a server sent unasked - is skipped rather than treated as a
// fault: this client has nothing it is waiting to do with either.
func (c *client) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var resp rpcResponse
		if json.Unmarshal([]byte(line), &resp) != nil {
			continue
		}
		var id int64
		if json.Unmarshal(resp.ID, &id) != nil {
			continue
		}

		c.mu.Lock()
		ch, ok := c.pending[id]
		if ok {
			delete(c.pending, id)
		}
		c.mu.Unlock()

		if ok {
			ch <- resp
		}
	}

	err := fmt.Errorf("mcp: the server's output ended")
	if scanner.Err() != nil {
		err = fmt.Errorf("mcp: read: %w", scanner.Err())
	}

	c.mu.Lock()
	c.readErr = err
	pending := c.pending
	c.pending = make(map[int64]chan rpcResponse)
	c.mu.Unlock()

	for _, ch := range pending {
		close(ch)
	}
}

// call sends a request and waits for its matching response, or for
// callTimeout to pass.
//
// A call that times out drops its own slot so the (possibly still arriving)
// response is discarded by readLoop rather than delivered to no one, but it
// does not stop the subprocess: a slow answer is not evidence the server is
// broken, and the next call gets its own fresh wait.
func (c *client) call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		c.forget(id)
		return nil, fmt.Errorf("mcp: encode %s: %w", method, err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		c.forget(id)
		return nil, fmt.Errorf("mcp: write %s: %w", method, err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			c.mu.Lock()
			readErr := c.readErr
			c.mu.Unlock()
			return nil, fmt.Errorf("mcp: %s: %w", method, readErr)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp: %s: %s (code %d)", method, resp.Error.Message, resp.Error.Code)
		}
		return resp.Result, nil

	case <-time.After(callTimeout):
		c.forget(id)
		return nil, fmt.Errorf("mcp: %s timed out after %s", method, callTimeout)
	}
}

// forget removes a call's slot without waiting for it further.
func (c *client) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// notify sends a JSON-RPC message with no reply expected.
func (c *client) notify(method string, params any) error {
	note := rpcNotification{JSONRPC: "2.0", Method: method, Params: params}
	data, err := json.Marshal(note)
	if err != nil {
		return fmt.Errorf("mcp: encode %s: %w", method, err)
	}
	data = append(data, '\n')
	_, err = c.stdin.Write(data)
	return err
}

// Close stops the subprocess, giving it a moment to exit on its own before it
// is killed outright.
func (c *client) Close() error {
	c.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		if c.cmd.Process != nil {
			c.cmd.Process.Kill()
		}
		<-done
	}
	return nil
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
