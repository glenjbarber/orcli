package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugLogRedactsConfiguredAndTokenShapedSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".orcli-debug.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer file.Close()

	log := &debugLog{file: file, keys: []string{"openrouter-secret", "notion-secret", "cloudflare-secret"}}
	log.record("interaction", map[string]any{
		"apiKey":        "short-field-secret",
		"message":       "configured openrouter-secret ntn_abcdef1234567890 and an opaque012345678901234567890123456789token",
		"tool_response": `{"access_token":"short-json-secret"}`,
	})

	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, data)
	}
	raw := string(data)
	for _, secret := range []string{"openrouter-secret", "notion-secret", "cloudflare-secret", "short-field-secret", "abcdef1234567890", "opaque012345678901234567890123456789token", "short-json-secret"} {
		if strings.Contains(raw, secret) {
			t.Errorf("debug log contains secret %q: %s", secret, raw)
		}
	}
	if !strings.Contains(raw, "[REDACTED]") {
		t.Errorf("debug log contains no redaction marker: %s", raw)
	}
}

func TestOpenDebugLogUsesPrivateModeAndRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	log, err := openDebugLog(dir, "configured-secret")
	if err != nil {
		t.Fatalf("openDebugLog: %v", err)
	}
	if err := log.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	path := filepath.Join(dir, ".orcli-debug.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("debug log mode = %#o, want %#o", got, want)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "target"), path); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if _, err := openDebugLog(dir); err == nil {
		t.Fatal("openDebugLog accepted a symlink")
	}
}

func TestTraceCommandEnablesRedactedCaptureAndStops(t *testing.T) {
	dir := t.TempDir()
	log := newDebugLog(dir, "configured-secret")
	log.record("before", map[string]any{"message": "not captured"})
	if _, err := os.Stat(log.path()); !os.IsNotExist(err) {
		t.Fatalf("dormant capture created a file: %v", err)
	}
	text, err := log.traceCommand("")
	if err != nil || !strings.HasPrefix(text, "trace is on: ") {
		t.Fatalf("trace on = %q, %v", text, err)
	}
	log.record("request", map[string]any{"message": "configured-secret"})
	text, err = log.traceCommand("status")
	if err != nil || !strings.Contains(text, log.path()) {
		t.Fatalf("trace status = %q, %v", text, err)
	}
	if text, err = log.traceCommand("off"); err != nil || text != "trace is off" {
		t.Fatalf("trace off = %q, %v", text, err)
	}
	log.record("after", map[string]any{"message": "not captured"})
	data, err := os.ReadFile(log.path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "configured-secret") || !strings.Contains(string(data), "[REDACTED]") {
		t.Fatalf("trace contents were not redacted: %s", data)
	}
	if strings.Contains(string(data), `"type":"after"`) {
		t.Fatalf("capture continued after /trace off: %s", data)
	}
}
