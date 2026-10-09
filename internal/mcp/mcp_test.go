package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// fakeServer returns a Server that launches this same test binary, re-exec'd
// into TestHelperProcess below, which speaks just enough MCP to drive these
// tests. This is the standard way to give exec-backed tests a subprocess
// without depending on a shell or another binary being on the host - see
// os/exec's own tests for the pattern this borrows.
func fakeServer(t *testing.T, script string) Server {
	t.Helper()
	return Server{
		Name:    "fake",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     []string{"GO_WANT_HELPER_PROCESS=1", "FAKE_MCP_SCRIPT=" + script},
	}
}

func TestManagerStartsInitializesAndReusesAClient(t *testing.T) {
	m := NewManager([]Server{fakeServer(t, "resources")})
	defer m.Close()

	c1, err := m.get("fake")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	c2, err := m.get("fake")
	if err != nil {
		t.Fatalf("get (second): %v", err)
	}
	if c1 != c2 {
		t.Fatal("get started a second subprocess instead of reusing the first")
	}
}

func TestManagerGetRefusesAnUnconfiguredServer(t *testing.T) {
	m := NewManager([]Server{fakeServer(t, "resources")})
	defer m.Close()

	if _, err := m.get("nope"); err == nil {
		t.Fatal("get accepted an unconfigured server")
	}
}

func TestListResourcesToolListsAcrossAPage(t *testing.T) {
	m := NewManager([]Server{fakeServer(t, "resources")})
	defer m.Close()

	set := m.ToolSet()
	lister := set[0]
	if lister.Name() != listResources {
		t.Fatalf("set[0].Name() = %s", lister.Name())
	}

	result := lister.Run(json.RawMessage(`{"server":"fake"}`))
	if result.Err != nil {
		t.Fatalf("Run: %v", result.Err)
	}
	if !strings.Contains(result.Content, "res://one") || !strings.Contains(result.Content, "res://two") {
		t.Fatalf("Content = %q, want both paged resources", result.Content)
	}
}

func TestListResourcesToolRefusesWithNoServerNamed(t *testing.T) {
	m := NewManager([]Server{fakeServer(t, "resources")})
	defer m.Close()

	result := m.ToolSet()[0].Run(json.RawMessage(`{}`))
	if result.Err == nil {
		t.Fatal("Run accepted a call naming no server")
	}
}

func TestReadResourceToolReadsTextAndReportsBinary(t *testing.T) {
	m := NewManager([]Server{fakeServer(t, "read")})
	defer m.Close()

	reader := m.ToolSet()[1]
	if reader.Name() != readResource {
		t.Fatalf("set[1].Name() = %s", reader.Name())
	}

	text := reader.Run(json.RawMessage(`{"server":"fake","uri":"res://text"}`))
	if text.Err != nil {
		t.Fatalf("Run (text): %v", text.Err)
	}
	if text.Content != "hello from the fake server" {
		t.Fatalf("Content = %q", text.Content)
	}

	binary := reader.Run(json.RawMessage(`{"server":"fake","uri":"res://binary"}`))
	if binary.Err != nil {
		t.Fatalf("Run (binary): %v", binary.Err)
	}
	if !strings.Contains(binary.Content, "binary resource") || !strings.Contains(binary.Content, "image/png") {
		t.Fatalf("Content = %q, want a binary-resource notice", binary.Content)
	}
}

func TestReadResourceToolRequiresAURI(t *testing.T) {
	m := NewManager([]Server{fakeServer(t, "read")})
	defer m.Close()

	result := m.ToolSet()[1].Run(json.RawMessage(`{"server":"fake"}`))
	if result.Err == nil {
		t.Fatal("Run accepted a call with no uri")
	}
}

func TestClientCallReportsAServerError(t *testing.T) {
	m := NewManager([]Server{fakeServer(t, "error")})
	defer m.Close()

	result := m.ToolSet()[0].Run(json.RawMessage(`{"server":"fake"}`))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "the fake server refused") {
		t.Fatalf("Run = %v, want the server's own error message", result.Err)
	}
}

func TestClientCallTimesOutAgainstAServerThatNeverAnswers(t *testing.T) {
	orig := callTimeout
	callTimeout = 200 * 1_000_000 // 200ms, as a time.Duration in nanoseconds.
	defer func() { callTimeout = orig }()

	m := NewManager([]Server{fakeServer(t, "silent")})
	defer m.Close()

	result := m.ToolSet()[0].Run(json.RawMessage(`{"server":"fake"}`))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "timed out") {
		t.Fatalf("Run = %v, want a timeout", result.Err)
	}
}

// TestHelperProcess is not a real test. It is re-exec'd as a subprocess by
// fakeServer above, and acts as a minimal MCP server for the scripts the
// tests above name.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 16<<20)
	out := os.Stdout

	writeLine := func(v any) {
		data, _ := json.Marshal(v)
		fmt.Fprintf(out, "%s\n", data)
	}

	script := os.Getenv("FAKE_MCP_SCRIPT")

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal([]byte(line), &req) != nil {
			continue
		}

		// A notification (no id) never gets a reply, same as a real server.
		if len(req.ID) == 0 {
			continue
		}

		switch req.Method {
		case "initialize":
			writeLine(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "fake", "version": "0.0.0"},
			}})

		case "resources/list":
			if script == "error" {
				writeLine(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "error": map[string]any{"code": -32000, "message": "the fake server refused"}})
				continue
			}
			if script == "silent" {
				continue
			}

			var params struct {
				Cursor string `json:"cursor"`
			}
			json.Unmarshal(req.Params, &params)
			if params.Cursor == "" {
				writeLine(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{
					"resources":  []map[string]any{{"uri": "res://one", "name": "One"}},
					"nextCursor": "page2",
				}})
			} else {
				writeLine(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{
					"resources": []map[string]any{{"uri": "res://two", "name": "Two", "description": "the second page"}},
				}})
			}

		case "resources/read":
			var params struct {
				URI string `json:"uri"`
			}
			json.Unmarshal(req.Params, &params)
			switch params.URI {
			case "res://text":
				writeLine(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{
					"contents": []map[string]any{{"uri": params.URI, "mimeType": "text/plain", "text": "hello from the fake server"}},
				}})
			case "res://binary":
				writeLine(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{
					"contents": []map[string]any{{"uri": params.URI, "mimeType": "image/png", "blob": "aGVsbG8="}},
				}})
			default:
				writeLine(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]any{"contents": []map[string]any{}}})
			}
		}
	}
}
