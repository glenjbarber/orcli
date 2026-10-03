package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
	// exactly as the model wrote it. ls, pwd and ps are the cases, and echo is the
	// case that makes the distinction matter: echo prints its arguments, so resolving
	// one as a path would replace the text the model wrote with a directory it did
	// not name.
	noPaths pathShape = iota

	// leadingPaths: every argument that is not an option is a path. This is the
	// conservative shape and it belongs to the writers: cat, rm and the rest.
	leadingPaths

	// firstPath: the first argument is a path. find and wc are the cases, where a
	// later argument is a pattern or a flag rather than a file.
	firstPath

	// finalPath: the last non-option argument is a path, and the rest are patterns or
	// flags. grep and rg are the cases.
	finalPath
)

// shellPermitted is the programs the shell tool may run.
//
// It is declared once and every other form is derived from it, so the list a refusal
// names, the map a lookup consults, and the schema a model is shown cannot drift apart.
//
// The order groups the build tools first, then the readers, then the writers. It is not
// sorted, because the declaration is the thing a reader reads, and a model shown a
// refusal looks for the name it wanted near what it already knows.
//
// Every name resolves by bare name through the path search. That is what makes the list
// portable rather than pinned to one host, and it is also what a reader should know
// before approving one: the program that runs is whichever binary of that name the
// session reaches first. A name not present on the host is a refusal from the search and
// not a failure of this client, so the list carries platform-specific programs without
// carrying a platform-specific claim that they exist.
//
// gh and rm are on the list for the opposite reason to the readers above, and the reason
// is that they write. A model repairing a tree needs to remove what a build left behind,
// and a model working on a repository needs to read and act on a pull request. What
// bounds them is the approval mode and the argument checks, not a rule of their own: a
// name on this list bounds what may be proposed and nothing more, and the arguments go
// to the program as an array, so there is no pipe, redirect or chain by which one could
// reach a program not named here.
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
	"dmesg",

	// Writers.
	"gh",
	"rm",
}

// shellPermittedMap is the lookup form of the list.
//
// It is derived in a variable initialiser so it cannot drift out of step with the list
// above. A second copy of the list, maintained by hand, is a list that is wrong the
// first time a program is added.
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
// itself. Both are single points of change rather than knowledge spread through the
// code: add the name to shellPermitted, give it a shape here, and the refusal, the
// schema and the tests all follow.
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

	// dmesg takes no path at all. Every argument is either an option or a value
	// belonging to one: the facility to filter on, a level, a column count, a
	// follow or not-follow mode, or a buffer to read. A facility reads as an
	// ordinary word, so any other shape would resolve it as a file name and refuse
	// the call, which is the defect the noPaths shape exists to prevent.
	"dmesg": noPaths,
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
// is bounded by the check on the paths it is given rather than by a refusal here. A reader
// who wants it refused should have it named, and that is a question for the decision
// maker rather than something to decide by omission.
var refusedOptions = map[string][]string{
	"find": {"-exec", "-execdir", "-fls", "-fprint"},

	// dmesg clears the ring buffer and writes to it, so -c and -r are refused by
	// name for the reason -delete is not refused on find: both destroy the evidence
	// the reader asked to read, and neither is recoverable afterwards.
	"dmesg": {"-c", "--clear", "-r", "--read-clear", "-C", "--read-clear"},
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
// It runs a program from the allowlist with an argument array. It never reaches a shell,
// so a pipe, a redirect, and a chain are not available: those are features of a shell,
// and offering them to a model would mean running one. The schema says so, so a model
// that asks for a pipeline learns it is not on offer rather than having its request split
// into arguments.
//
// A call is bounded in two ways, and both are set here rather than at the call site: a
// single command may not run past shellTimeout, and its output may not exceed outputLimit.
// The bounds live in exec.go with the program they apply to, and this tool names the
// deadline it wants.
type Shell struct {
	// Dir is the working directory a subprocess runs in. Every path argument is
	// resolved against it and refused if it leaves.
	Dir string

	// Approval is the mode the reader has set: ask, allow, or deny.
	//
	// It is carried here so the checks and the question are in one place, and it is
	// consulted before anything runs rather than after.
	Approval string
}

// NewShell returns a shell tool contained by dir, in the asking mode.
func NewShell(dir string) *Shell {
	return &Shell{Dir: dir, Approval: ApprovalAsk}
}

// Name returns the tool name.
func (s *Shell) Name() string { return "shell" }

// Describe returns the schema.
//
// The description is built from the list rather than written out beside it, so a program
// added to the list cannot be missing from what a model is told. A model told only what
// the tool can do will ask for a pipe, and a model told it cannot will use the programs
// it has.
func (s *Shell) Describe() Schema {
	return Schema{
		Type: "function",
		Function: FunctionSpec{
			Name:        "shell",
			Description: shellDescription(),
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

// shellDescription renders the model-facing description from the list.
//
// It is a function and not a constant because the list is the single source of truth. A
// description written out beside the list is a second copy, and a second copy is wrong the
// first time a program is added.
//
// The bounds are named in it rather than left for a model to discover by hitting them. A
// model that knows a command may run for two minutes and write a mebibyte will keep a
// large command to one program and ask for the rest separately, where a model that finds
// the limit by having the output cut learns only that something went wrong.
func shellDescription() string {
	return fmt.Sprintf("run one of these %d programs in the current directory: %s. "+
		"This is not a shell: pipes, redirects, and command chains are not available, "+
		"and each argument is passed to the program as written. Paths are relative to "+
		"the working directory, and an absolute path or one that leaves the tree is "+
		"refused. A pattern is judged by the directory leading to it, so *.go is "+
		"allowed and ../*.go is not. gh and rm write, find may not be given -exec, and "+
		"dmesg may not be given -c since that clears the buffer being read. "+
		"one command runs for at most %s and writes at most %d bytes, and a command "+
		"past either bound is stopped and reported rather than truncated. %s",
		len(shellPermitted), permittedPrograms(), shellTimeout, outputLimit,
		gitSubcommandDescription())
}

// shellArgs is the decoded body of a shell call.
type shellArgs struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Run runs the program and returns what it produced.
//
// The order of the checks is the order the failures are worth reporting in. The program
// is settled against the list first, because a program that would be refused outright is
// not something to interrupt a reader about: a model asking for curl gets an instant
// answer and does not make the reader close a question box to learn that nothing was
// going to run. Then the arguments, since a question naming a command that would reach
// outside the tree is a question about something the reader cannot see. Then the approval
// mode, which is the only check here that can be answered yes.
//
// A failure to start, a failure inside the program, a program that ran past its deadline
// and a program that wrote past the cap are all reported the same way: with whatever it
// managed to write attached. A tool that ran and failed has still told the model
// something, and throwing that away would make a model retry a call whose answer was
// already on the wire.
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

	program, err := permitted(a.Command)
	if err != nil {
		return Result{Err: err}
	}

	args, err := s.checkArgs(program, a.Args)
	if err != nil {
		return Result{Err: err}
	}

	mode, err := s.approval()
	if err != nil {
		return Result{Err: err}
	}
	if mode == ApprovalDeny {
		return Result{Err: &ApprovalError{
			Program: program,
			Mode:    ApprovalDeny,
			Reason:  "the reader has set the mode to deny",
		}}
	}

	path, err := resolveProgram(program)
	if err != nil {
		return Result{Err: err}
	}

	out, err := runProgram(path, args, s.Dir, shellTimeout)
	if err != nil {
		if len(out) == 0 {
			return Result{Err: fmt.Errorf("tools: shell %s: %w", program, err)}
		}
		return Result{
			Content: string(out),
			Err:     fmt.Errorf("tools: shell %s: %w", program, err),
		}
	}
	return Result{Content: string(out)}
}

// approval returns the mode the reader has set, and reports one that is not a mode.
//
// An unset mode is refused rather than treated as ask. A session that never read a
// configuration is a session that cannot say what it would do, and defaulting to the
// asking mode would make a missing file look like a deliberate choice.
func (s *Shell) approval() (string, error) {
	if s.Approval == "" {
		return "", &ApprovalError{
			Reason: fmt.Sprintf("no approval mode is set, so the reader is asked; "+
				"it is one of %s", approvalModeNames()),
		}
	}
	mode, err := parseApproval(s.Approval)
	if err != nil {
		return "", &ApprovalError{Mode: s.Approval, Reason: err.Error()}
	}
	return mode, nil
}

// permitted reports whether a program is on the list, and names the list in the
// refusal so a model learns what it may ask for.
func permitted(program string) (string, error) {
	if shellPermittedMap[program] {
		return program, nil
	}
	return "", fmt.Errorf("%w: %s is not permitted: this tool runs %s",
		ErrRefused, program, permittedPrograms())
}

// checkArgs refuses an option or a marker its program may not be given, and resolves
// every argument the program's shape says is a path.
//
// An option is passed through untouched. A dash is what separates an option from a path,
// and an argument carrying one is the flag it says it is however much it resembles a
// file name. A non-option argument is a path only where the program's shape says one is,
// and is otherwise passed through exactly as the model wrote it.
//
// A path carrying a wildcard is judged by the directory leading to the wildcard and
// handed to the program unchanged, since no shell is read and the program is what expands
// it. Every other path is resolved against the working directory and refused if it
// leaves.
func (s *Shell) checkArgs(command string, args []string) ([]string, error) {
	shape, known := pathShapes[command]
	if !known {
		return nil, fmt.Errorf("%w: %s has no declared path shape", ErrRefused, command)
	}

	// git is checked against its own allowlist as well, since it is on the shell
	// list and the subcommand would otherwise be reachable only by asking the
	// shell rather than the git tool.
	if command == "git" {
		if err := gitThroughShell(args); err != nil {
			return nil, err
		}
	}

	out := make([]string, 0, len(args))
	for i, arg := range args {
		if isRefusedOption(command, arg) {
			return nil, fmt.Errorf("%w: %s may not be given %s", ErrRefused, command, arg)
		}
		if marker, refused := isRefusedMarker(command, arg); refused {
			return nil, fmt.Errorf("%w: %s may not be given an argument carrying %q, "+
				"which is how this program runs something or writes a file of its own",
				ErrRefused, command, marker)
		}
		if !isPathArgument(shape, i, args) {
			out = append(out, arg)
			continue
		}
		if _, _, wild := cutWildcard(arg); wild {
			pattern, err := checksWildcard(s.Dir, arg)
			if err != nil {
				return nil, err
			}
			out = append(out, pattern)
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
	if strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("%w: %s names a path, and a program is named by bare name",
			ErrRefused, name)
	}
	path, err := lookPath(name)
	if err != nil {
		return "", fmt.Errorf("tools: %s was not found on PATH: %w", name, err)
	}
	return path, nil
}
