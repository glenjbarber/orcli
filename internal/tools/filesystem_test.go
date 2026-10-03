package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// body renders a call body for a tool.
func body(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return data
}

// byName finds a tool in a set.
func byName(tools []Tool, name string) Tool {
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	return nil
}

// TestFilesystemReadsInsideTheRoot is the ordinary case.
func TestFilesystemReadsInsideTheRoot(t *testing.T) {
	dir := tree(t)

	fs, err := NewFilesystem(dir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	defer fs.Close()

	result := invoke(byName(fs.Tools(), readFile), body(t, map[string]any{"path": "one.txt"}))
	if result.Err != nil {
		t.Fatalf("read_file: %v", result.Err)
	}
	if got, want := result.Content, "in one.txt"; got != want {
		t.Errorf("content is %q, want %q", got, want)
	}
}

// TestFilesystemRefusesToWalkOut is the rule the descriptor exists for.
//
// Every one of these is refused by the descriptor rather than by a check in the
// tool, which is the property that makes the descriptor the right mechanism.
func TestFilesystemRefusesToWalkOut(t *testing.T) {
	dir := tree(t)

	fs, err := NewFilesystem(dir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	defer fs.Close()

	for _, path := range []string{
		"..",
		"../one.txt",
		"../../etc/passwd",
		"sub/../../outside.txt",
	} {
		t.Run(path, func(t *testing.T) {
			result := invoke(byName(fs.Tools(), readFile), body(t, map[string]any{"path": path}))
			if result.Err == nil {
				t.Errorf("read_file %q succeeded, want a refusal", path)
			}
		})
	}
}

// TestFilesystemRefusesToWalkOutOnWrite checks the write side as well.
//
// A read refusal is a refusal the model can recover from. A write outside the
// tree is the one that matters, and the descriptor refuses it the same way.
func TestFilesystemRefusesToWalkOutOnWrite(t *testing.T) {
	dir := tree(t)

	fs, err := NewFilesystem(dir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	defer fs.Close()

	result := invoke(byName(fs.Tools(), writeFile),
		body(t, map[string]any{"path": "../written.txt", "content": "no"}))
	if result.Err == nil {
		t.Fatal("write_file above the root succeeded, want a refusal")
	}

	// Nothing may have been written outside the root either.
	parent := filepath.Dir(dir)
	if _, err := os.Stat(filepath.Join(parent, "written.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a file appeared above the root after a refused write")
	}
}

// TestFilesystemWritesInsideTheRoot covers the ordinary write.
func TestFilesystemWritesInsideTheRoot(t *testing.T) {
	dir := tree(t)

	fs, err := NewFilesystem(dir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	defer fs.Close()

	result := invoke(byName(fs.Tools(), writeFile),
		body(t, map[string]any{"path": "sub/new.txt", "content": "written"}))
	if result.Err != nil {
		t.Fatalf("write_file: %v", result.Err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "sub", "new.txt"))
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if string(got) != "written" {
		t.Errorf("the file holds %q, want %q", got, "written")
	}
}

// TestFilesystemListsADirectory covers list_dir, which is the one tool that had
// to change because os.Root has no ReadDir.
func TestFilesystemListsADirectory(t *testing.T) {
	dir := tree(t)

	fs, err := NewFilesystem(dir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	defer fs.Close()

	result := invoke(byName(fs.Tools(), listDir), body(t, map[string]any{"path": "sub"}))
	if result.Err != nil {
		t.Fatalf("list_dir: %v", result.Err)
	}
	if got := strings.TrimSpace(result.Content); got != "three.txt" {
		t.Errorf("listing is %q, want three.txt", got)
	}

	// A directory is marked, so a reader can tell what can be listed from what
	// cannot.
	result = invoke(byName(fs.Tools(), listDir), body(t, map[string]any{"path": "."}))
	if result.Err != nil {
		t.Fatalf("list_dir .: %v", result.Err)
	}
	if !strings.Contains(result.Content, "sub/") {
		t.Errorf("listing is %q, want the directory marked with a slash", result.Content)
	}
}

// TestFilesystemRefusesAPathWithNoName covers the call that named no path.
func TestFilesystemRefusesAPathWithNoName(t *testing.T) {
	dir := tree(t)

	fs, err := NewFilesystem(dir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	defer fs.Close()

	for _, name := range []string{readFile, writeFile, listDir} {
		result := invoke(byName(fs.Tools(), name), body(t, map[string]any{}))
		if result.Err == nil {
			t.Errorf("%s with no path succeeded, want a refusal", name)
		}
	}
}

// TestFilesystemRefusesArgumentsThatAreNotAnObject covers the shared decode.
func TestFilesystemRefusesArgumentsThatAreNotAnObject(t *testing.T) {
	dir := tree(t)

	fs, err := NewFilesystem(dir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	defer fs.Close()

	result := invoke(byName(fs.Tools(), readFile), json.RawMessage(`["one.txt"]`))
	if result.Err == nil {
		t.Error("an array of arguments was accepted, want a refusal")
	}
}

// TestFilesystemReportsAMissingFileAsAbsent covers telling the two refusals
// apart, since a model can act on one and not the other.
func TestFilesystemReportsAMissingFileAsAbsent(t *testing.T) {
	dir := tree(t)

	fs, err := NewFilesystem(dir)
	if err != nil {
		t.Fatalf("NewFilesystem: %v", err)
	}
	defer fs.Close()

	result := invoke(byName(fs.Tools(), readFile), body(t, map[string]any{"path": "nope.txt"}))
	if result.Err == nil {
		t.Fatal("reading a missing file succeeded")
	}
	if !IsNotExist(result.Err) {
		t.Errorf("the failure is %v, want it reported as an absent file", result.Err)
	}

	// A path outside the root is not an absent file.
	result = invoke(byName(fs.Tools(), readFile), body(t, map[string]any{"path": "../nope.txt"}))
	if result.Err == nil {
		t.Fatal("reading above the root succeeded")
	}
	if IsNotExist(result.Err) {
		t.Error("a path outside the root was reported as an absent file")
	}
}

// TestFilesystemWithoutARootProducesAResult covers the guarantee at its edge.
//
// A caller with no working directory open gets a result, not a panic.
func TestFilesystemWithoutARootProducesAResult(t *testing.T) {
	var fs *Filesystem

	for _, name := range []string{readFile, writeFile, listDir} {
		tool := byName(fs.Tools(), name)
		result := invoke(tool, body(t, map[string]any{"path": "one.txt"}))
		if result.Err == nil {
			t.Errorf("%s with no root succeeded, want a refusal", name)
		}
	}

	if got := fs.Root(); got != "" {
		t.Errorf("Root is %q, want it empty for no filesystem", got)
	}
	if err := fs.Close(); err != nil {
		t.Errorf("Close on nothing returned %v, want nil", err)
	}
}

// TestInvokeAlwaysProducesAResult is the guarantee in the package documentation.
//
// An unknown name, arguments that are not an object, a body that failed, and a
// body that panicked are all reported as a result. A call producing no result
// leaves a turn waiting for something that never arrives.
func TestInvokeAlwaysProducesAResult(t *testing.T) {
	if result := invoke(nil, nil); result.Err == nil {
		t.Error("invoking nothing produced no error, want a result carrying one")
	}

	panicking := toolFunc{name: "panics", run: func(json.RawMessage) Result {
		panic("something went wrong")
	}}

	result := invoke(panicking, nil)
	if result.Err == nil {
		t.Fatal("a panicking tool produced no error, want the panic recovered")
	}
	if !strings.Contains(result.Err.Error(), "panics") {
		t.Errorf("the failure is %q, want it to name the tool", result.Err)
	}
}

// toolFunc is a Tool whose behaviour a test supplies.
type toolFunc struct {
	name string
	run  func(json.RawMessage) Result
}

func (t toolFunc) Name() string                 { return t.name }
func (t toolFunc) Describe() Schema             { return Schema{Type: "function"} }
func (t toolFunc) Run(a json.RawMessage) Result { return t.run(a) }
