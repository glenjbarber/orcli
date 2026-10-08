package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// debugLog is a private, line-oriented capture of the conversation stream.
type debugLog struct {
	mu   sync.Mutex
	file *os.File
	dir  string
	keys []string
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(\b(api[_-]?key|api[_-]?token|access[_-]?token|refresh[_-]?token|authorization|auth|client[_-]?secret|private[_-]?key|secret|password|credential|token)\b\s*["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,}]+)`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]+=*`),
	regexp.MustCompile(`\bsk-or-v1-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`\bsk-(proj|ant|live|test)-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`\b(rk|pk)_(live|test)_[A-Za-z0-9]{8,}`),
	regexp.MustCompile(`\b(ntn|secret)_[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`\bcf-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
	regexp.MustCompile(`\b[A-Za-z0-9_+/=-]{32,}\b`),
}

func openDebugLog(dir string, keys ...string) (*debugLog, error) {
	d := &debugLog{dir: dir, keys: keys}
	if err := d.enable(true); err != nil {
		return nil, err
	}
	return d, nil
}

func newDebugLog(dir string, keys ...string) *debugLog {
	return &debugLog{dir: dir, keys: keys}
}

func (d *debugLog) path() string {
	return filepath.Join(d.dir, ".orcli-debug.jsonl")
}

func (d *debugLog) enable(truncate bool) error {
	if d == nil {
		return fmt.Errorf("debug capture is unavailable")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file != nil {
		return nil
	}
	path := d.path()
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("debug log path is not a regular file: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect debug log: %w", err)
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if truncate {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return fmt.Errorf("open debug log: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("secure debug log: %w", err)
	}
	d.file = f
	return nil
}

func (d *debugLog) close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}

func (d *debugLog) record(kind string, value any) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file == nil {
		return
	}
	body, err := json.Marshal(value)
	if err != nil {
		body = []byte(`"<unserializable>"`)
	} else {
		var decoded any
		if json.Unmarshal(body, &decoded) == nil {
			body, err = json.Marshal(d.redactValue(decoded))
			if err != nil {
				body = []byte(`"<unserializable>"`)
			}
		}
	}
	_, _ = fmt.Fprintf(d.file, "{\"time\":%q,\"type\":%q,\"data\":%s}\n", time.Now().UTC().Format(time.RFC3339Nano), kind, body)
}

func (d *debugLog) enabled() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.file != nil
}

func (d *debugLog) traceCommand(args string) (string, error) {
	if d == nil {
		return "", fmt.Errorf("/trace is unavailable")
	}
	action := strings.TrimSpace(args)
	if action == "" {
		action = "on"
	}
	switch action {
	case "status":
		if d.enabled() {
			return fmt.Sprintf("trace is on: %s", d.path()), nil
		}
		return "trace is off", nil
	case "on":
		if err := d.enable(false); err != nil {
			return "", err
		}
		d.record("trace_started", map[string]any{"path": d.path()})
		return fmt.Sprintf("trace is on: %s", d.path()), nil
	case "off":
		if !d.enabled() {
			return "trace is off", nil
		}
		d.record("trace_stopped", map[string]any{"path": d.path()})
		if err := d.close(); err != nil {
			return "", fmt.Errorf("close trace log: %w", err)
		}
		return "trace is off", nil
	default:
		return "", fmt.Errorf("/trace must be on, off, or status")
	}
}

func (d *debugLog) redactValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if sensitiveField(key) {
				v[key] = "[REDACTED]"
			} else {
				v[key] = d.redactValue(item)
			}
		}
		return v
	case []any:
		for i := range v {
			v[i] = d.redactValue(v[i])
		}
		return v
	case string:
		return d.redact(v)
	default:
		return value
	}
}

func sensitiveField(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	compact := strings.ReplaceAll(key, "_", "")
	for _, part := range []string{"api_key", "api_token", "access_token", "refresh_token", "client_secret", "private_key", "secret_key", "ssh_key", "password", "credential", "authorization", "cookie", "session", "secret", "token"} {
		part = strings.ReplaceAll(part, "_", "")
		if compact == part || (len(part) > 3 && strings.HasSuffix(compact, part)) {
			return true
		}
	}
	return compact == "key"
}

func (d *debugLog) redact(s string) string {
	for _, key := range d.keys {
		if key != "" {
			s = strings.ReplaceAll(s, key, "[REDACTED]")
		}
	}
	for i, pattern := range secretPatterns {
		replacement := `[REDACTED]`
		if i == 0 {
			replacement = `${1}[REDACTED]`
		}
		s = pattern.ReplaceAllString(s, replacement)
	}
	return s
}
