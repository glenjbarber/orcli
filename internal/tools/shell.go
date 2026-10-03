package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrRefused is returned when a call is refused rather than failing.
//
// It is a refusal and not a fault: a model asked for something it may not have is told
// so and carries on, which is different from a program that ran and failed.
var ErrRefused = errors.New("tools: refused")

// pathShape says how a program finds its path arguments.
//
// Deciding whether an argument is a path needs the program, not the argument. ls takes
// no path, echo takes no path at all and prints what it is given, grep takes a pattern
// before its file, and rm takes paths and flags interleaved. Resolving every argument as
// a path would rewrite the first three of those into something the model did not write,
// which is a way of changing the call rather than of checking it.
type pathShape int

const (
	// noPaths: the program takes no path at all. Every argument is passed through
	// exactly as the model wrote it. ls, pwd and ps are the cases, and echo is the case
	// that makes the distinction matter: echo prints its arguments, so resolving one as
	// a path would replace the text the model wrote with a directory it did not name.
	noPaths pathShape = iota

	// leadingPaths: every argument that is not an option is a path. This is the
	// conservative shape and it belongs to the writers: cat, rm and the rest.
	leadingPaths

	// firstPath: the first argument is a path. find and wc are the cases, where a later
	// argument is a pattern or a flag rather than a file.
	firstPath

	// finalPath: the last non-option argument is a path, and the rest are patterns or
	// flags. grep and rg are the cases.
	finalPath
)

// shellPermitted is the programs the shell tool may run.
//
// The list is a list of names, resolved by bare name through PATH, and it is fixed here
// rather than read from the configuration file. What a program may do is a property of
// this package; what a reader is asked before it happens is decided by the interface,
// and neither can widen the other.
//
// The order groups the build tools first, then the readers, then the writers. It is not
// sorted, because the declaration is the thing a reader reads, and a model shown a
// refusal looks for the name it wanted near what it already knows.
//
// gh and rm are on the list for the opposite reason to the readers above, and the reason
// is that they write. A model repairing a tree needs to remove what a build left behind,
// and a model working on a repository needs to read and act on a pull request. What
// bounds them is not a rule of their own: a name on this list bounds what may be
// proposed and nothing more, since every call is still put to the reader, and the
// arguments go to the program as an array, so there is no pipe, redirect or chain by
// which one could reach a program not named here.
var shellPermitted = []string{
	// Build and toolchain.
	"go",
	"gofmt",
	"make",
	"bmake",
	"git",
	"errcheck",
	"gosec",
	"govulncheck",
	"protoc-gen-go",
	"protoc-gen-go-grpc",
	"staticcheck",

	// Readers and inspection.
	"ls",
	"cat",
	"pwd",
	"echo",
	"grep",
	"rg",
	"find",
	"wc",
	"head",
	"tail",
	"sed",
	"awk",
	"stat",
	"file",
	"diff",
	"hexdump",
	"od",
	"jq",
	"ps",

	// Writers.
	"gh",
	"rm",
}

// shellPermittedMap is the lookup form of the list.
//
// It is derived in a variable initialiser so it cannot drift out of step with the list
// above. A second copy of the list, maintained by hand, is a list that is wrong
// somewhere by the first time a program is added.
var shellPermittedMap = func() map[string]bool {
	m := make(map[string]bool, len(shellPermitted))
	for _, name := range shellPermitted {
		m[name] = true
	}
	return m
}()

// permittedPrograms renders the list for a refusal.
//
// The refusal names the list, so a model that asked for something outside it learns what
// it may ask for instead of learning only that it was refused.
func permittedPrograms() string { return strings.Join(shellPermitted, ", ") }

// pathShapes is the path shape of each permitted program.
//
// Every program on the list has an entry, and a test enforces it. A program without one
// is refused rather than run unchecked, since a gap in this table is not a licence to
// run a program whose arguments nobody has thought about.
//
// This is the one table a person adding a program has to touch, alongside the list
// itself and the description below. Both are single points of change rather than
// knowledge spread through the code: add the name to shellPermitted, give it a shape
// here, and the refusal, the schema and the tests all follow.
var pathShapes = map[string]pathShape{
	// The toolchain writes through whatever it is pointed at, and a path that leaves
	// the tree is caught by leadingPaths for the same reason rm is.
	"go":                 leadingPaths,
	"gofmt":              leadingPaths,
	"make":               leadingPaths,
	"bmake":              leadingPaths,
	"git":                leadingPaths,
	"errcheck":           leadingPaths,
	"gosec":              leadingPaths,
	"govulncheck":        leadingPaths,
	"protoc-gen-go":      leadingPaths,
	"protoc-gen-go-grpc": leadingPaths,
	"staticcheck":        leadingPaths,

	"ls":      noPaths,
	"pwd":     noPaths,
	"ps":      noPaths,
	"echo":    noPaths,
	"cat":     leadingPaths,
	"sed":     leadingPaths,
	"awk":     leadingPaths,
	"stat":    leadingPaths,
	"diff":    leadingPaths,
	"rm":      leadingPaths,
	"gh":      leadingPaths,
	"grep":    finalPath,
	"rg":      finalPath,
	"find":    firstPath,
	"wc":      firstPath,
	"head":    firstPath,
	"tail":    firstPath,
	"file":    firstPath,
	"hexdump": firstPath,
	"od":      firstPath,
	"jq":      firstPath,
}

// refusedOptions are the options each program may not be given, by name.
//
// A name here is refused outright, wherever it appears in the argument list, since the
// program would otherwise be handed the command to run and the containment would be
// describing the wrong thing.
//
// The list is short and the reasoning is uniform: these are the options that turn a
// reader into something that runs a command. find is the only program with any, and
// -delete is deliberately absent from that set. It removes files and runs nothing, so it
// is bounded by the check on the paths it is given rather than by a refusal here. A
// reader who wants it refused should have it named, and that is a question for the
// decision maker rather than something to decide by omission.
var refusedOptions = map[string][]string{
	"find": {"-exec", "-execdir", "-fls", "-fprint"},
}

// isRefusedOption reports whether an argument is an option a program may not be given.
//
// The match is exact. A bundled or abbreviated form is not matched, because a program
// that accepts one is accepting a different option and this list names options rather
// than prefixes.
func isRefusedOption(program, arg string) bool {
	for _, name := range refusedOptions[program] {
		if arg == name {
			return true
		}
	}
	return false
}

// Shell is the shell tool, contained by comparison rather than by a descriptor.
//
// It runs a program from the allowlist with an argument array. It never reaches a
// shell, so a pipe, a redirect, and a chain are not available: those are features of a
// shell, and offering them would mean running one. The schema says so, so a model that
// asks for a pipeline learns it is not on offer rather than having its request split
// into arguments.
type Shell struct {
	// Dir is the working directory a subprocess runs in. Every path argument is resolved
	// against it and refused if it leaves.
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
// The description says plainly that this is not a shell and that a pipeline is not
// available. A model told only what the tool can do will ask for a pipe, and a model
// told it cannot will use the programs it has.
func (s *Shell) Describe() Schema {
	return Schema{
		Type: "function",
		Function: FunctionSpec{
			Name:        "shell",
			Description: shellParameters,
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

// shellParameters is the model-facing description of the tool.
//
// The list is spelled out a second time here, since this is the string a model reads and
// it has to read as a sentence rather than as an identifier. It is held honest by a test
// rather than by construction, because building it from the list would produce a
// comma-separated identifier where a sentence belongs.
const shellParameters = "run one of these programs in the current directory: " +
	"go, gofmt, make, bmake, git, errcheck, gosec, govulncheck, " +
	"protoc-gen-go, protoc-gen-go-grpc, staticcheck, ls, cat, pwd, echo, grep, " +
	"rg, find, wc, head, tail, sed, awk, stat, file, diff, hexdump, od, jq, " +
	"ps, gh, rm. " +
	"This is not a shell: pipes, redirects, and command chains are not available, " +
	"and each argument is passed to the program as written. Paths are relative to the " +
	"working directory, and a path outside it, or an absolute path, is refused. " +
	"gh and rm write, and find may not be given -exec."

// shellArgs is the decoded body of a shell call.
type shellArgs struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Run runs the program and returns what it produced.
//
// A failure to start and a failure inside the program are both reported, and both carry
// the output the program managed to write. A tool that ran and failed has still told the
// model something, and throwing that away would make a model retry a call whose answer
// was already on the wire.
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

	program, args, err := s.check(a.Command, a.Args)
	if err != nil {
		return Result{Err: err}
	}

	out, err := runProgram(program, args, s.Dir)
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

// check refuses a program outside the list, an option that program may not be given,
// and an argument that leaves the working directory.
//
// The order is the order the failures are worth reporting in. A program that is not
// permitted is refused by name before the filesystem is asked about it and before a
// reader is interrupted, since a call that was never going to run is not something to ask
// somebody about. The refusal names the list, so a model that asked for something outside
// it learns what it may ask for.
//
// An option is passed through untouched. A dash is what separates an option from a path,
// and an argument carrying one is the flag it says it is however much it resembles a file
// name. A non-option argument is a path only where the program's shape says one is, and
// is otherwise passed through exactly as the model wrote it.
func (s *Shell) check(command string, args []string) (string, []string, error) {
	if !shellPermittedMap[command] {
		return "", nil, fmt.Errorf("tools: %s is not permitted: this tool runs %s",
			command, permittedPrograms())
	}

	for _, arg := range args {
		if isRefusedOption(command, arg) {
			return "", nil, fmt.Errorf("%w: %s may not be given %s", ErrRefused, command, arg)
		}
	}

	shape, known := pathShapes[command]
	if !known {
		return "", nil, fmt.Errorf("%w: %s has no declared path shape", ErrRefused, command)
	}

	program, err := resolveProgram(command)
	if err != nil {
		return "", nil, err
	}

	out := make([]string, 0, len(args))
	for i, arg := range args {
		if !isPathArgument(shape, i, args) {
			out = append(out, arg)
			continue
		}
		resolved, err := containment(s.Dir, arg)
		if err != nil {
			return "", nil, err
		}
		out = append(out, resolved)
	}
	return program, out, nil
}

// isPathArgument reports whether the argument at position i is a path.
//
// An option never is, however much it resembles a file name, since a dash is what
// separates the two. Beyond that it depends on the shape: a program that takes leading
// paths has one wherever it is not an option, a program that takes a first path has one
// only at the front, a program that takes a final path has one only at the end, and a
// program that takes none has none at all.
func isPathArgument(shape pathShape, i int, args []string) bool {
	if isOption(args[i]) {
		return false
	}
	switch shape {
	case leadingPaths:
		return true
	case firstPath:
		return i == 0
	case finalPath:
		// The last non-option argument, so that `grep -I pattern file` checks the file
		// and not the pattern.
		last := -1
		for j, arg := range args {
			if !isOption(arg) {
				last = j
			}
		}
		return i == last
	default:
		return false
	}
}

// resolveProgram finds a permitted program by bare name through the search path.
//
// The name is never resolved as a path, so a model asking for /bin/sh is refused by the
// list check before this is reached. Resolution by bare name is what makes the list
// portable rather than pinned to one host, and a decision maker should know that the
// program which runs is whichever binary of that name the session reaches first.
func resolveProgram(name string) (string, error) {
	path, err := lookPath(name)
	if err != nil {
		return "", fmt.Errorf("tools: %s was not found on PATH: %w", name, err)
	}
	return path, nil
}
