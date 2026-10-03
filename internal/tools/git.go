package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrRefusedGit is returned when a git call is refused.
//
// It is distinct from ErrRefused because the git tool has reasons of its own that a
// reader is told about differently: a subcommand that is not on the allowlist is a
// question about what this client does with a repository, and a wildcard is a question
// about what a command would touch.
var ErrRefusedGit = errors.New("tools: refused")

// gitPermitted is the subcommands the git tool may run.
//
// The list is an allowlist and not a denylist, since a denylist of subcommands is a
// list that has to be extended every time git adds one. A model asking for a
// subcommand that is not here is refused by name, and the refusal names what is
// offered, since a model chooses its next call from that message.
//
// The commit, push, worktree and merge subcommands are here because a model working on
// a repository needs them, and refusing them would mean the decision maker does the
// work at a second prompt. What bounds them is the approval the reader is asked about
// on every call, and not a rule of their own: a name on this list bounds what may be
// proposed and nothing more.
var gitPermitted = []string{
	// Reading.
	"status",
	"log",
	"diff",
	"show",
	"blame",
	"branch",
	"tag",
	"remote",
	"rev-parse",
	"ls-files",
	"ls-tree",
	"describe",
	"shortlog",
	"cat-file",
	"config",

	// Writing.
	"add",
	"commit",
	"push",
	"fetch",
	"merge",
	"rebase",
	"stash",
	"cherry-pick",
	"revert",
	"worktree",
	"switch",
	"restore",
	"checkout",
}

// gitPermittedMap is the lookup form of the list.
//
// It is derived rather than written out, so a subcommand added to the list is permitted
// without a second edit that could be forgotten.
var gitPermittedMap = func() map[string]bool {
	m := make(map[string]bool, len(gitPermitted))
	for _, name := range gitPermitted {
		m[name] = true
	}
	return m
}()

// gitRefusedSubcommands are the subcommands refused by name, with the reason.
//
// They are refused rather than absent, so the refusal can say this client does not do
// that rather than implying the subcommand does not exist. A model told a subcommand is
// unknown will try another spelling; a model told it is refused will move on.
//
// Each one either removes something the repository cannot recover or rewrites history
// for every commit at once. There is no approval answer that makes either a reasonable
// default, which is why they are refused here rather than left to the reader per call.
var gitRefusedSubcommands = map[string]string{
	"clean":            "it removes files git is not tracking, which cannot be recovered from the repository",
	"reset":            "it moves the current branch, and --hard discards work in the tree",
	"filter-branch":    "it rewrites history for every commit, and there is no undo",
	"filter-repo":      "it rewrites history for every commit, and there is no undo",
	"gc":               "it removes objects, so one a reflog still names can become unreachable",
	"prune":            "it deletes tracking branches whose remote branch is gone",
	"am":               "it applies a patch, and the result of a patch is hard to see beforehand",
	"submodule":        "it runs commands from another repository, which is not containment this tool can give",
	"credential":       "it handles a credential, and a credential is not a thing a model should hold",
	"credential-cache": "it handles a credential, and a credential is not a thing a model should hold",
}

// gitRefusedOptions are the options that may not be given to any subcommand, with the
// reason.
//
// The pattern is the same as the shell's, and the reasoning is the same: an option
// here is a way of making a command reach further than an argument check can see. These
// are the options that widen a pathspec to the whole repository or to every matching
// file, and they are refused by name rather than by cleaning the argument, since a glob
// is not a path and cleaning one produces a path that is not a file the pattern would
// have matched.
var gitRefusedOptions = map[string]string{
	"--all":      "it names every path in the repository rather than a path",
	"--glob":     "it names files by pattern rather than by path",
	"--branches": "it names branches rather than a path",
	"--tags":     "it names tags rather than a path",
	"--remotes":  "it names remote branches rather than a path",
	"--not":      "it inverts a pathspec, so what is named is what is not named",
	"--exclude":  "it excludes by pattern, which is a wildcard this tool will not expand",
}

// gitRefusedOptionsBySubcommand are options refused for one subcommand only.
var gitRefusedOptionsBySubcommand = map[string]map[string]string{
	// checkout --orphan creates a branch with no history, and the files staged in
	// the tree are the only copy of them afterwards.
	"checkout": {"--orphan": "it creates a branch with no history, leaving the tree as the only copy"},
	// switch --discard-changes drops modifications in the working tree.
	"switch": {"--discard-changes": "it discards modifications in the working tree"},
}

// isWildcard reports whether an argument carries a glob character.
//
// A wildcard is not an option, so it is a path the caller wrote in the form that means
// many files at once. It is reported rather than resolved: a pattern has no single
// target, so cleaning one produces a path that is not a file the pattern would have
// matched, and refusing it by name is the only answer that does not depend on knowing
// what is in the directory.
func isWildcard(arg string) bool {
	return !isOption(arg) && strings.ContainsAny(arg, "*?[")
}

// Git is the git tool, contained by comparison rather than by a descriptor.
//
// It runs a subcommand from an allowlist with an argument array and never reaches a
// shell. Every path is resolved against the working directory and refused if it leaves,
// and a wildcard is refused by name rather than resolved.
type Git struct {
	// Dir is the working directory a subprocess runs in.
	Dir string
}

// NewGit returns a git tool contained by dir.
func NewGit(dir string) *Git {
	return &Git{Dir: dir}
}

// Name returns the tool name.
func (g *Git) Name() string { return "git" }

// Describe returns the schema.
func (g *Git) Describe() Schema {
	return Schema{
		Type: "function",
		Function: FunctionSpec{
			Name:        "git",
			Description: gitParameters,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subcommand": map[string]any{
						"type":        "string",
						"description": "the subcommand to run",
					},
					"args": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "the arguments to pass, as a list and not as a single string",
					},
				},
				"required": []string{"subcommand"},
			},
		},
	}
}

// gitParameters is the model-facing description of the tool.
//
// The subcommand list is spelled out here rather than built from the declaration, since
// this has to read as a sentence to a model. A test holds the two together.
const gitParameters = "run one of these git subcommands in the current directory: " +
	"status, log, diff, show, blame, branch, tag, remote, rev-parse, ls-files, " +
	"ls-tree, describe, shortlog, cat-file, config, add, commit, push, fetch, " +
	"merge, rebase, stash, cherry-pick, revert, worktree, switch, restore, " +
	"checkout. " +
	"This is not a shell: pipes, redirects, and command chains are not available, " +
	"and each argument is passed to git as written. Paths are relative to the " +
	"working directory, and a path outside it, or an absolute path, is refused. A " +
	"wildcard is refused rather than expanded. git clean, reset, filter-branch, " +
	"filter-repo, gc, prune, am, submodule, and the credential commands are refused."

// gitArgs is the decoded body of a git call.
type gitArgs struct {
	Subcommand string   `json:"subcommand"`
	Args       []string `json:"args"`
}

// Run runs the subcommand and returns what it produced.
//
// A failure inside git is reported with whatever output it managed to write, since git
// explains most of its own failures and throwing that away would make a model retry a
// call whose answer was already on the wire.
func (g *Git) Run(raw json.RawMessage) Result {
	var a gitArgs
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return Result{Err: fmt.Errorf("tools: git: the arguments are not a JSON object: %w", err)}
		}
	}
	sub := strings.TrimSpace(a.Subcommand)
	if sub == "" {
		return Result{Err: fmt.Errorf("tools: git: no subcommand was given")}
	}
	if g == nil || g.Dir == "" {
		return Result{Err: fmt.Errorf("tools: git: no working directory is open")}
	}

	program, args, err := g.check(sub, a.Args)
	if err != nil {
		return Result{Err: err}
	}

	out, err := runProgram(program, args, g.Dir)
	if err != nil {
		if len(out) == 0 {
			return Result{Err: fmt.Errorf("tools: git %s: %w", sub, err)}
		}
		return Result{
			Content: string(out),
			Err:     fmt.Errorf("tools: git %s: %w", sub, err),
		}
	}
	return Result{Content: string(out)}
}

// check refuses a subcommand outside the list, an option that subcommand may not be
// given, a wildcard, and an argument that leaves the working directory.
//
// The order is the order the failures are worth reporting in. The subcommand is checked
// first, before the filesystem is asked about anything, since a call that was never
// going to run is not worth resolving a path for.
func (g *Git) check(subcommand string, args []string) (string, []string, error) {
	if reason, refused := gitRefusedSubcommands[subcommand]; refused {
		return "", nil, fmt.Errorf("%w: git %s is not run by this tool, because %s",
			ErrRefusedGit, subcommand, reason)
	}
	if !gitPermittedMap[subcommand] {
		return "", nil, fmt.Errorf("tools: git %s is not permitted: this tool runs %s",
			subcommand, permittedSubcommands())
	}

	for _, arg := range args {
		if reason, refused := refusedGitOption(subcommand, arg); refused {
			return "", nil, fmt.Errorf("%w: git %s may not be given %s: %s",
				ErrRefusedGit, subcommand, arg, reason)
		}
		if isWildcard(arg) {
			return "", nil, fmt.Errorf("%w: git %s may not be given the wildcard %s; a "+
				"pattern is not a path and this tool will not expand one",
				ErrRefusedGit, subcommand, arg)
		}
	}

	program, err := lookPath("git")
	if err != nil {
		return "", nil, fmt.Errorf("tools: git was not found on PATH: %w", err)
	}

	// The subcommand is not an option, so it is passed through untouched.
	out := make([]string, 0, len(args)+1)
	out = append(out, subcommand)

	separator := false
	for _, arg := range args {
		// A -- is carried through and turns off path resolution for everything
		// after it, which is git's own convention and the reason a pathspec
		// cannot be a flag. It is recorded rather than added by this package,
		// since a command that already has one must not be given a second.
		if arg == "--" {
			separator = true
			out = append(out, arg)
			continue
		}
		if separator || isOption(arg) {
			out = append(out, arg)
			continue
		}
		resolved, err := containment(g.Dir, arg)
		if err != nil {
			return "", nil, err
		}
		out = append(out, resolved)
	}

	// A -- is placed before the first path when the model did not write one and
	// there is a path to place it before. An argument that looks like a flag must
	// not be able to become one by being read as a pathspec.
	if !separator {
		if at := firstPathArg(args); at >= 0 {
			out = insertSeparator(out, at+1)
		}
	}
	return program, out, nil
}

// firstPathArg returns the index of the first argument that is a path, or -1.
//
// An option is not a path, however much it resembles one, and an option that takes a
// value is a case this does not attempt: a git subcommand with an option taking a path
// value would need the option's own table, and none of the subcommands here takes one
// before its pathspec in a form that could be mistaken for a flag.
func firstPathArg(args []string) int {
	for i, arg := range args {
		if !isOption(arg) {
			return i
		}
	}
	return -1
}

// insertSeparator puts a -- at the given index of the argument list.
func insertSeparator(args []string, at int) []string {
	out := make([]string, 0, len(args)+1)
	out = append(out, args[:at]...)
	out = append(out, "--")
	return append(out, args[at:]...)
}

// refusedGitOption reports whether an argument is an option the subcommand may not be
// given, and why.
func refusedGitOption(subcommand, arg string) (string, bool) {
	if reason, ok := gitRefusedOptions[arg]; ok {
		return reason, true
	}
	if bySubcommand, ok := gitRefusedOptionsBySubcommand[subcommand]; ok {
		if reason, ok := bySubcommand[arg]; ok {
			return reason, true
		}
	}
	return "", false
}

// permittedSubcommands renders the list for a refusal.
func permittedSubcommands() string { return strings.Join(gitPermitted, ", ") }
