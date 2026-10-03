package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrRefusedGit is returned when a git call is refused.
//
// It is a distinct error rather than ErrRefused because the git tool has reasons
// of its own that a reader is told about differently: a subcommand that is not on the
// allowlist is a question about what this client does with a repository, and a
// wildcard is a question about what a command would touch.
var ErrRefusedGit = errors.New("tools: refused a git call")

// gitPermitted is the subcommands the git tool may run.
//
// It is declared once and every other form is derived from it, so the list a
// refusal names, the map a lookup consults, and the schema a model is shown cannot
// drift apart.
//
// The list is an allowlist and not a denylist, since a denylist of subcommands is a
// list that has to be extended every time git adds one. A model asking for a
// subcommand that is not here is refused by name, and the refusal names what is
// offered, since a model chooses its next call from that message.
//
// The commit, push, worktree and merge subcommands are here because a model working on
// a repository needs them, and refusing them would mean the decision maker does the
// work at a second prompt. What bounds them is the approval mode and the argument
// checks, not a rule of their own: a name on this list bounds what may be proposed and
// nothing more.
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
// It is derived rather than written out, so a subcommand added to the list is
// permitted without a second edit that could be forgotten.
var gitPermittedMap = func() map[string]bool {
	m := make(map[string]bool, len(gitPermitted))
	for _, name := range gitPermitted {
		m[name] = true
	}
	return m
}()

// permittedSubcommands renders the list for a refusal.
func permittedSubcommands() string { return strings.Join(gitPermitted, ", ") }

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
// The pattern is the same as the shell's, and the reasoning is the same: an option here
// is a way of making a command reach further than an argument check can see. These are
// the options that widen a pathspec to the whole repository or to every matching file,
// and they are refused by name rather than by cleaning the argument, since a glob is
// not a path and cleaning one produces a path that is not a file the pattern would have
// matched.
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
	// checkout --orphan creates a branch with no history, and the files staged in the
	// tree are the only copy of them afterwards.
	"checkout": {"--orphan": "it creates a branch with no history, leaving the tree as the only copy"},
	// switch --discard-changes drops modifications in the working tree.
	"switch": {"--discard-changes": "it discards modifications in the working tree"},
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

// Git is the git tool, contained by comparison rather than by a descriptor.
//
// It runs a subcommand from the allowlist with an argument array and never reaches a
// shell. Every path is resolved against the working directory and refused if it leaves,
// and a wildcard is refused by name rather than resolved.
type Git struct {
	// Dir is the working directory a subprocess runs in.
	Dir string

	// Approval is the mode the reader has set: ask, allow, or deny.
	//
	// It is consulted before anything runs rather than after, and an empty value is
	// refused rather than treated as ask, for the reason given on the shell tool.
	Approval string
}

// NewGit returns a git tool contained by dir, in the asking mode.
func NewGit(dir string) *Git {
	return &Git{Dir: dir, Approval: ApprovalAsk}
}

// Name returns the tool name.
func (g *Git) Name() string { return "git" }

// Describe returns the schema.
//
// The description is built from the list rather than written out beside it, so a
// subcommand added to the list cannot be missing from what a model is told.
func (g *Git) Describe() Schema {
	return Schema{
		Type: "function",
		Function: FunctionSpec{
			Name:        "git",
			Description: gitDescription(),
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

// gitDescription renders the model-facing description from the list.
//
// The refused subcommands are named too, since a model that is not told a subcommand
// is refused will ask for it and be refused again.
func gitDescription() string {
	refused := make([]string, 0, len(gitRefusedSubcommands))
	for name := range gitRefusedSubcommands {
		refused = append(refused, name)
	}
	sortStrings(refused)

	return fmt.Sprintf("run one of these %d git subcommands in the current directory: %s. "+
		"This is not a shell: pipes, redirects, and command chains are not available, "+
		"and each argument is passed to git as written. Paths are relative to the "+
		"working directory, and a path outside it, or an absolute path, is refused. A "+
		"wildcard is refused rather than expanded. These subcommands are refused: %s.",
		len(gitPermitted), permittedSubcommands(), strings.Join(refused, ", "))
}

// sortStrings sorts in place, so a refusal names subcommands in a settled order.
//
// The order of a map iteration is not settled, and a model reading a list that is in a
// different order each turn is reading something that looks like it changed.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// gitArgs is the decoded body of a git call.
type gitArgs struct {
	Subcommand string   `json:"subcommand"`
	Args       []string `json:"args"`
}

// Run runs the subcommand and returns what it produced.
//
// The order of the checks is the order the failures are worth reporting in: the
// subcommand first, since a call that was never going to run is not worth resolving a
// path for, then the arguments, then the approval mode.
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

	mode, err := g.approval()
	if err != nil {
		return Result{Err: err}
	}
	if mode == ApprovalDeny {
		return Result{Err: &ApprovalError{
			Program: "git " + sub,
			Mode:    ApprovalDeny,
			Reason:  "the reader has set the mode to deny",
		}}
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

// approval returns the mode the reader has set, and reports one that is not a mode.
func (g *Git) approval() (string, error) {
	if g.Approval == "" {
		return "", &ApprovalError{
			Program: "git",
			Reason: fmt.Sprintf("no approval mode is set, so the reader is asked; "+
				"it is one of %s", approvalModeNames()),
		}
	}
	mode, err := parseApproval(g.Approval)
	if err != nil {
		return "", &ApprovalError{Program: "git", Mode: g.Approval, Reason: err.Error()}
	}
	return mode, nil
}

// check refuses a subcommand outside the list, an option that subcommand may not be
// given, a wildcard, and an argument that leaves the working directory.
func (g *Git) check(subcommand string, args []string) (string, []string, error) {
	if reason, refused := gitRefusedSubcommands[subcommand]; refused {
		return "", nil, fmt.Errorf("%w: git %s is not run by this tool, because %s",
			ErrRefusedGit, subcommand, reason)
	}
	if !gitPermittedMap[subcommand] {
		return "", nil, fmt.Errorf("%w: git %s is not permitted: this tool runs %s",
			ErrRefusedGit, subcommand, permittedSubcommands())
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
		// A -- is carried through and turns off path resolution for everything after
		// it, which is git's own convention and the reason a pathspec cannot be a
		// flag. It is recorded rather than added by this package, since a command
		// that already has one must not be given a second.
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

	// A -- is placed before the first path when the model did not write one and there
	// is a path to place it before.
	if !separator {
		if at := firstPathArg(args); at >= 0 {
			out = insertSeparator(out, at+1)
		}
	}
	return program, out, nil
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

// firstPathArg returns the index of the first argument that is a path, or -1.
//
// An option is not a path, however much it resembles one, and an option that takes a
// value is a case this does not attempt: a subcommand with an option taking a path
// value would need the option's own table.
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
