package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// lookPath finds a program on PATH.
//
// It is a field so a test can point the shell tool at a directory holding
// stand-in programs, rather than at whatever happens to be installed on the
// machine running the suite. The shipped behaviour is exec.LookPath, which is
// what a reader's machine has.
var lookPath = exec.LookPath

// ErrRefused is returned when a call is refused rather than failing.
//
// It is a refusal and not a fault: a model asked for something it may not have
// is told so and carries on, which is different from a program that ran and
// failed.
var ErrRefused = errors.New("tools: refused")

// shellPrograms is the programs the shell tool may run.
//
// The list is a list of names, resolved by bare name through PATH, and it is
// fixed here rather than read from the configuration file. What a program is
// allowed to do is a property of this package; what a reader is asked before it
// happens is decided by the interface, and neither can widen the other.
//
// Every one of these is a program that reads or reports. None of them is a
// shell, none of them takes a flag that takes a command, and none of them can be
// talked into running something else.
var shellPrograms = []string{
	"cat",
	"echo",
	"false",
	"find",
	"file",
	"grep",
	"head",
	"ls",
	"pwd",
	"sort",
	"stat",
	"tail",
	"true",
	"uname",
	"wc",
	"which",
}

// Shell is the shell tool, contained by comparison rather than by a descriptor.
//
// It runs a program from an allowlist with an argument array. It never reaches a
// shell, so a pipe, a redirect, and a chain are not available: those are
// features of a shell, and offering them would mean running one. The schema says
// so, so a model that asks for a pipeline learns it is not on offer rather than
// having its request split into arguments.
type Shell struct {
	// Dir is the working directory a subprocess runs in. Every path argument
	// is resolved against it and refused if it leaves.
	Dir string
}

// NewShell returns a shell tool contained by dir.
func NewShell(dir string) *Shell {
	return &Shell{Dir: dir}
}

// Name returns the tool name.
func (s *Shell) Name() string { return "shell" }

// Describe returns the schema.
//
// The description says plainly that this is not a shell and that a pipeline is
// not available. A model told only what the tool can do will ask for a pipe,
// and a model told it cannot will use the programs it has.
func (s *Shell) Describe() Schema {
	return Schema{
		Type: "function",
		Function: FunctionSpec{
			Name: "shell",
			Description: fmt.Sprintf(
				"run one of these programs in the current directory: %s. "+
					"This is not a shell: pipes, redirects, and command chains are "+
					"not available, and each argument is passed to the program as "+
					"written. Paths are relative to the working directory, and a "+
					"path outside it is refused.",
				strings.Join(shellPrograms, ", ")),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "the program to run, by bare name",
					},
					"args": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "the arguments to pass, as a list and not as a single string",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

// shellArgs is the decoded body of a shell call.
type shellArgs struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Run runs the program and returns what it produced.
//
// A failure to start and a failure inside the program are both reported, and
// both carry the output the program managed to write. A tool that ran and failed
// has still told the model something, and throwing that away would make a model
// retry a call whose answer was already on the wire.
func (s *Shell) Run(raw json.RawMessage) Result {
	var a shellArgs
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return Result{Err: fmt.Errorf("tools: shell: the arguments are not a JSON object: %w", err)}
		}
	}
	if strings.TrimSpace(a.Command) == "" {
		return Result{Err: fmt.Errorf("tools: shell: no command was given")}
	}
	if s == nil || s.Dir == "" {
		return Result{Err: fmt.Errorf("tools: shell: no working directory is open")}
	}

	path, err := resolveProgram(a.Command, shellPrograms)
	if err != nil {
		return Result{Err: err}
	}

	args, err := s.checkArgs(a.Command, a.Args)
	if err != nil {
		return Result{Err: err}
	}

	out, err := runProgram(path, args, s.Dir)
	if err != nil {
		if len(out) == 0 {
			return Result{Err: fmt.Errorf("tools: shell %s: %w", a.Command, err)}
		}
		return Result{
			Content: string(out),
			Err:     fmt.Errorf("tools: shell %s: %w", a.Command, err),
		}
	}
	return Result{Content: string(out)}
}

// runProgram runs a resolved program in dir with an argument array.
//
// exec.Command is given the program as a resolved path and the arguments as
// separate entries, so nothing inside an argument is ever interpreted: a pipe in
// an argument is a character in a string and not an operator. The environment is
// set explicitly rather than inherited, since a subprocess that inherits the
// reader's environment inherits whatever secrets happen to be in it.
func runProgram(path string, args []string, dir string) ([]byte, error) {
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = environment()
	return cmd.Output()
}

// checkArgs resolves every argument that is a path and refuses one that leaves
// the working directory.
//
// Which arguments are paths is decided per program rather than guessed. ls takes
// no paths at all, and grep takes a pattern that very often is not a path, so a
// uniform rule would either refuse ordinary calls or permit the one that
// matters. A program with no entry here is refused rather than assumed: a gap in
// this package is not a licence to run unchecked.
func (s *Shell) checkArgs(command string, args []string) ([]string, error) {
	shape, known := pathArguments[command]
	if !known {
		return nil, fmt.Errorf("%w: %s has no declared path arguments", ErrRefused, command)
	}

	out := make([]string, 0, len(args))
	for i, arg := range args {
		if !isPathArgument(shape, i, args) {
			out = append(out, arg)
			continue
		}
		resolved, err := containment(s.Dir, arg)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

// pathShape says how a program finds its path arguments.
type pathShape int

const (
	// none: the program takes no path at all.
	none pathShape = iota

	// first: the first argument is a path.
	first

	// last: the final argument is a path, when it is not a flag. grep is the
	// case this exists for: `grep pattern file` names a file and
	// `grep -r pattern .` does not, and no fixed index tells the two apart.
	last
)

// pathArguments is the path shape of each allowed program.
//
// Every program on the allowlist is listed. A program with no paths is
// distinguished from a program whose shape has not been worked out, so that a
// missing entry is refused rather than silently treated as unrestricted.
var pathArguments = map[string]pathShape{
	"cat":   first,
	"echo":  none,
	"false": none,
	"find":  first,
	"file":  first,
	"grep":  last,
	"head":  first,
	"ls":    none,
	"pwd":   none,
	"sort":  first,
	"stat":  first,
	"tail":  first,
	"true":  none,
	"uname": none,
	"wc":    first,
	"which": first,
}

// isPathArgument reports whether the argument at position i is a path.
//
// A flag is never a path. A model sending -I to grep is sending a flag, and
// treating it as a file name would refuse a call that is perfectly ordinary.
func isPathArgument(shape pathShape, i int, args []string) bool {
	switch shape {
	case none:
		return false
	case first:
		return i == 0
	case last:
		if i != len(args)-1 {
			return false
		}
		return !strings.HasPrefix(args[i], "-")
	default:
		return false
	}
}
