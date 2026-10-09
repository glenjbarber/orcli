package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/glenjbarber/orcli/internal/tui"
)

// contextFileLog remembers which context file paths this process has read, in the
// order it first read them, so a reader can ask /context later rather than having to
// scroll back through the log or trust an unverified claim about what was loaded.
//
// "Context file" here means a file sent to the model as part of a turn's own
// introduction - today that is AGENTS.md, read by introduction() in ask.go - rather
// than every file a tool call happens to open along the way. A record is kept once per
// distinct path, in the order first read, since a reader asking what grounded a
// session wants the list of sources, not a tally of how many turns re-read an
// unchanged file.
type contextFileLog struct {
	mu    sync.Mutex
	seen  map[string]bool
	paths []string
}

// globalContextLog is the one instance introduction() records into and /context
// reads from.
//
// It is package-level rather than threaded through ask's and the dispatcher's
// constructors because the two meet nowhere else: introduction() is a free function
// called from inside the closure askWithTaskIntegrations returns, and the dispatcher
// is a separate value built in main.go. Both exist once per running process today, so
// one shared log for the process is the list a reader typing /context actually wants,
// and it is simpler than widening two long parameter lists to pass the same pointer
// through for a feature that is read-mostly and has no need of per-test isolation
// beyond what resetGlobalContextLog gives it.
var globalContextLog = newContextFileLog()

func newContextFileLog() *contextFileLog {
	return &contextFileLog{seen: make(map[string]bool)}
}

// record adds path, resolved to an absolute form, to the log, unless that same file
// is already in it.
//
// The path is made absolute before the duplicate check so that two reads of the same
// file from different working directories still count as one entry; introduction()
// only ever reads AGENTS.md relative to the session's own working directory, but a
// path recorded as given, unresolved, would let a second session in a different
// directory look like a second distinct file when it is not.
func (l *contextFileLog) record(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen[abs] {
		return
	}
	l.seen[abs] = true
	l.paths = append(l.paths, abs)
}

// list returns the recorded paths in the order they were first read.
//
// It returns a copy rather than the log's own slice, since the caller is rendering
// text for a reader and has no business holding a reference the log might still
// append to.
func (l *contextFileLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.paths))
	copy(out, l.paths)
	return out
}

// resetGlobalContextLog clears globalContextLog for a test that needs to assert on
// it without seeing entries an earlier test in the same package left behind.
func resetGlobalContextLog() {
	globalContextLog = newContextFileLog()
}

// context is the handler for /context: it reports which context file paths this
// session has read so far, so a reader can check what actually grounded the model's
// answers rather than assume.
func (d *dispatcher) context(args string) (tui.Result, error) {
	if strings.TrimSpace(args) != "" {
		return tui.Result{}, fmt.Errorf("/context takes no arguments")
	}

	paths := globalContextLog.list()
	if len(paths) == 0 {
		return tui.Result{Text: "no context files have been read yet"}, nil
	}

	var b strings.Builder
	b.WriteString("context files read this session:\n")
	for _, p := range paths {
		b.WriteString("  " + p + "\n")
	}
	return tui.Result{Text: strings.TrimRight(b.String(), "\n")}, nil
}
