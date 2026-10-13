package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// stdioTransport is one running local MCP server subprocess, spoken to over
// newline-delimited JSON-RPC on its stdin and stdout.
//
// A single background goroutine owns the read side and demultiplexes
// responses to the call that is waiting for each one by ID, which is what
// lets a call time out without corrupting a read another call still needs:
// the reader never stops, it only stops being waited on.
type stdioTransport struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResponse
	readErr error
}

// newStdioTransport launches a server's subprocess and begins reading its
// output.
func newStdioTransport(s Server) (*stdioTransport, error) {
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

	t := &stdioTransport{cmd: cmd, stdin: stdin, pending: make(map[int64]chan rpcResponse)}
	go t.readLoop(stdout)
	return t, nil
}

// readLoop reads one JSON-RPC message per line for the life of the
// subprocess, and delivers each to whichever call is waiting for its ID. A
// line this client cannot parse, or one carrying no numeric ID - a
// notification a server sent unasked - is skipped rather than treated as a
// fault: this client has nothing it is waiting to do with either.
func (t *stdioTransport) readLoop(r io.Reader) {
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

		t.mu.Lock()
		ch, ok := t.pending[id]
		if ok {
			delete(t.pending, id)
		}
		t.mu.Unlock()

		if ok {
			ch <- resp
		}
	}

	err := fmt.Errorf("mcp: the server's output ended")
	if scanner.Err() != nil {
		err = fmt.Errorf("mcp: read: %w", scanner.Err())
	}

	t.mu.Lock()
	t.readErr = err
	pending := t.pending
	t.pending = make(map[int64]chan rpcResponse)
	t.mu.Unlock()

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
func (t *stdioTransport) call(method string, params any) (json.RawMessage, error) {
	t.mu.Lock()
	t.nextID++
	id := t.nextID
	ch := make(chan rpcResponse, 1)
	t.pending[id] = ch
	t.mu.Unlock()

	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		t.forget(id)
		return nil, fmt.Errorf("mcp: encode %s: %w", method, err)
	}
	data = append(data, '\n')

	if _, err := t.stdin.Write(data); err != nil {
		t.forget(id)
		return nil, fmt.Errorf("mcp: write %s: %w", method, err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			t.mu.Lock()
			readErr := t.readErr
			t.mu.Unlock()
			return nil, fmt.Errorf("mcp: %s: %w", method, readErr)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp: %s: %s (code %d)", method, resp.Error.Message, resp.Error.Code)
		}
		return resp.Result, nil

	case <-time.After(callTimeout):
		t.forget(id)
		return nil, fmt.Errorf("mcp: %s timed out after %s", method, callTimeout)
	}
}

// forget removes a call's slot without waiting for it further.
func (t *stdioTransport) forget(id int64) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

// notify sends a JSON-RPC message with no reply expected.
func (t *stdioTransport) notify(method string, params any) error {
	note := rpcNotification{JSONRPC: "2.0", Method: method, Params: params}
	data, err := json.Marshal(note)
	if err != nil {
		return fmt.Errorf("mcp: encode %s: %w", method, err)
	}
	data = append(data, '\n')
	_, err = t.stdin.Write(data)
	return err
}

// Close stops the subprocess, giving it a moment to exit on its own before it
// is killed outright.
func (t *stdioTransport) Close() error {
	t.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- t.cmd.Wait() }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		if t.cmd.Process != nil {
			t.cmd.Process.Kill()
		}
		<-done
	}
	return nil
}
