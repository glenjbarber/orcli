package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideRoot is returned when a path resolves outside the working
// directory.
//
// It is a refusal rather than a fault, and the difference matters: a model
// asked for a path it cannot have is told so, and a caller can carry on.
var ErrOutsideRoot = errors.New("tools: the path is outside the working directory")

// Filesystem is the file tools, contained by an open descriptor.
//
// The root is opened once, on the working directory, and does not move for the
// session. There is no command that changes the working directory, so a path
// that resolved inside the root at startup still resolves inside it later.
//
// A path of ".." is refused by the descriptor itself, not by a check in this
// file: every method on os.Root resolves the path beneath the directory the
// descriptor names, and there is nowhere above it to reach.
type Filesystem struct {
	root *os.Root
	dir  string
}

// NewFilesystem opens a root on dir.
//
// The root cannot be opened is reported to the caller rather than refused here,
// because a session without file tools is a session with less in it rather than
// a session that cannot run. The interface decides how to say so, and says it
// once at startup rather than on every turn.
func NewFilesystem(dir string) (*Filesystem, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("tools: resolve %s: %w", dir, err)
	}

	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("tools: open the working directory: %w", err)
	}
	return &Filesystem{root: root, dir: abs}, nil
}

// Close releases the root descriptor.
//
// A session calls this once, on the way out. A root left open is a descriptor
// held until the process exits, which is a leak a long session would otherwise
// carry for its whole life.
func (f *Filesystem) Close() error {
	if f == nil || f.root == nil {
		return nil
	}
	return f.root.Close()
}

// Root returns the path the root was opened on, for a message that names it.
func (f *Filesystem) Root() string {
	if f == nil {
		return ""
	}
	return f.dir
}

// The names the file tools are called by.
const (
	readFile  = "read_file"
	writeFile = "write_file"
	listDir   = "list_dir"
)

// Tools returns the file tools, in the order they are offered.
//
// The order is the order they are most likely to be wanted in, since a model
// reading a list sees them in the order it was given them.
func (f *Filesystem) Tools() []Tool {
	return []Tool{
		&readFileTool{fs: f},
		&writeFileTool{fs: f},
		&listDirTool{fs: f},
	}
}

// fileTool is what the three file tools share: the schema shape, and the check
// that a path was given at all.
type fileTool struct {
	fs *Filesystem
}

// noPath is the refusal for a call that named no path.
//
// It is named here rather than left to each tool, so the three tools refuse the
// same way.
func noPath(name string) Result {
	return Result{Err: fmt.Errorf("tools: %s: no path was given", name)}
}

// pathArgs reads the path out of a call body.
func (t *fileTool) pathArgs(name string, args json.RawMessage) (string, Result) {
	a, err := decode(args)
	if err != nil {
		return "", Result{Err: fmt.Errorf("tools: %s: %w", name, err)}
	}
	if strings.TrimSpace(a.Path) == "" {
		return "", noPath(name)
	}
	return a.Path, Result{}
}

// noRoot is the refusal for a filesystem whose root was never opened.
//
// A caller that has no working directory open gets a result carrying this, not a
// nil dereference, because the guarantee is that a call always produces one.
func noRoot(name string) Result {
	return Result{Err: fmt.Errorf("tools: %s: no working directory is open", name)}
}

// readFileTool reads a file inside the root.
type readFileTool struct{ fileTool }

// Name returns the tool name.
func (t *readFileTool) Name() string { return readFile }

// Describe returns the schema. The path is relative to the working directory,
// because a model sending an absolute path deserves to be refused with a reason
// rather than left to guess which root was meant.
func (t *readFileTool) Describe() Schema {
	return Schema{
		Type: "function",
		Function: FunctionSpec{
			Name:        readFile,
			Description: "read a file as text",
			Parameters:  pathSchema("the file to read"),
		},
	}
}

// Run reads the file and returns its contents.
func (t *readFileTool) Run(args json.RawMessage) Result {
	path, bad := t.pathArgs(readFile, args)
	if bad.Err != nil {
		return bad
	}
	if t.fs == nil || t.fs.root == nil {
		return noRoot(readFile)
	}

	data, err := t.fs.root.ReadFile(path)
	if err != nil {
		return Result{Err: fmt.Errorf("tools: %s %s: %w", readFile, path, err)}
	}
	return Result{Content: string(data)}
}

// writeFileTool writes a file inside the root.
type writeFileTool struct{ fileTool }

// Name returns the tool name.
func (t *writeFileTool) Name() string { return writeFile }

// Describe returns the schema. It says the file is replaced whole, since a model
// that does not know that will read a partial write as the whole file.
func (t *writeFileTool) Describe() Schema {
	return Schema{
		Type: "function",
		Function: FunctionSpec{
			Name:        writeFile,
			Description: "write a file as text, replacing it whole",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "the file to write"},
					"content": map[string]any{"type": "string", "description": "what to write"},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

// Run writes the file and returns what it wrote.
func (t *writeFileTool) Run(args json.RawMessage) Result {
	a, err := decode(args)
	if err != nil {
		return Result{Err: fmt.Errorf("tools: %s: %w", writeFile, err)}
	}
	if strings.TrimSpace(a.Path) == "" {
		return noPath(writeFile)
	}
	if t.fs == nil || t.fs.root == nil {
		return noRoot(writeFile)
	}

	// The parent is created because a model asked to write a file under a
	// directory it has not made is asking for a file, not for an error about a
	// directory. A path that leaves the root is refused by the root itself.
	if dir := filepath.Dir(a.Path); dir != "." && dir != "/" {
		if err := t.fs.root.MkdirAll(dir, 0o755); err != nil {
			return Result{Err: fmt.Errorf("tools: %s %s: %w", writeFile, a.Path, err)}
		}
	}
	if err := t.fs.root.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
		return Result{Err: fmt.Errorf("tools: %s %s: %w", writeFile, a.Path, err)}
	}
	return Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path)}
}

// listDirTool lists a directory inside the root.
type listDirTool struct{ fileTool }

// Name returns the tool name.
func (t *listDirTool) Name() string { return listDir }

// Describe returns the schema.
func (t *listDirTool) Describe() Schema {
	return Schema{
		Type: "function",
		Function: FunctionSpec{
			Name:        listDir,
			Description: "list a directory",
			Parameters:  pathSchema("the directory to list"),
		},
	}
}

// Run lists the directory, one entry per line, with a trailing slash on a
// directory so a reader can tell what can be listed from what cannot.
//
// The root has no ReadDir, so the directory is opened through it and read from
// the handle. Reading through the handle rather than through the root's FS is
// deliberate: fs.ReadDir would take a path the descriptor has not checked, and
// the whole point of the descriptor is that every path is checked.
func (t *listDirTool) Run(args json.RawMessage) Result {
	path, bad := t.pathArgs(listDir, args)
	if bad.Err != nil {
		return bad
	}
	if t.fs == nil || t.fs.root == nil {
		return noRoot(listDir)
	}

	dir, err := t.fs.root.Open(path)
	if err != nil {
		return Result{Err: fmt.Errorf("tools: %s %s: %w", listDir, path, err)}
	}
	defer dir.Close()

	entries, err := dir.ReadDir(-1)
	if err != nil {
		return Result{Err: fmt.Errorf("tools: %s %s: %w", listDir, path, err)}
	}

	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			b.WriteString(e.Name() + "/\n")
			continue
		}
		b.WriteString(e.Name() + "\n")
	}
	return Result{Content: b.String()}
}

// pathSchema is the parameter shape every path-taking tool uses.
//
// One shape for three tools is deliberate. A model given three different
// argument shapes for the same idea of a path will eventually send one tool the
// arguments meant for another, and the error that arrives will name the wrong
// tool.
func pathSchema(what string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": what + ", relative to the working directory",
			},
		},
		"required": []string{"path"},
	}
}

// IsNotExist reports whether an error is a refusal because the path is absent,
// as opposed to a refusal because the path is outside the root.
//
// The two are worth telling apart at the call site: a file that is not there is
// something a model can act on by creating it, and a path outside the root is
// something no answer will make possible.
func IsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
