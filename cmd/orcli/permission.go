package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/tools"
	"github.com/glenjbarber/orcli/internal/tui"
)

// approve is the handler for `/approve`.
//
// It mirrors /level's shape, the way /level's own doc comment names it mirroring
// /model: an empty argument reports the mode in force rather than changing it, and a
// word that is not one of the three is refused by name rather than guessed at. What
// it reports and sets is `s.Options().Approval`, the same field ask.go reads as
// `approval := string(s.Options().Approval)` before a turn runs a tool - this is the
// one place that value is chosen rather than only read.
//
// The three words are ask, allow and deny. The command table's own summary says
// "ask|allow|refuse", which is the word a reader reaching for a mnemonic might type,
// but internal/tools.ApprovalModes, internal/tui's own Approval constants and
// internal/config's are unanimous: there is no "refuse" mode, there is "deny", and a
// handler that accepted "refuse" would be accepting a fourth spelling the rest of the
// tree does not recognise. "refuse" is told apart from an unknown word anyway, so a
// reader who typed what the summary suggests is pointed at "deny" by name rather than
// handed the bare list every other typo gets.
//
// A mode that is set is written to the configuration file as well as the session, for
// the reason modelHandler's own doc comment gives for /model: the frame draws what the
// session is answering with, and a command that wrote only the file, or only the
// session, would leave the two disagreeing until the next restart or the next turn.
func (d *dispatcher) approve(args string) (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/approve needs an open interface")
	}

	want := strings.ToLower(strings.TrimSpace(args))
	if want == "" {
		return tui.Result{Text: reportApproval(d.session.Options().Approval)}, nil
	}

	if want == "refuse" {
		return tui.Result{}, fmt.Errorf(
			"/approve refuse is not a mode; the mode that refuses every call without asking is /approve %s",
			tools.ApprovalDeny)
	}

	mode, err := config.ParseApproval(want)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/approve %s is not a mode; the modes are %s",
			want, approvalModeNames())
	}

	path, err := configPath()
	if err != nil {
		return tui.Result{}, err
	}
	if err := config.WriteApproval(path, mode); err != nil {
		return tui.Result{}, err
	}

	d.session.SetApproval(tui.Approval(mode))
	return tui.Result{Text: fmt.Sprintf("the approval mode is %s", mode)}, nil
}

// reportApproval is what `/approve` with no argument prints.
func reportApproval(mode tui.Approval) string {
	if mode == "" {
		return "no approval mode is set; the modes are " + approvalModeNames()
	}
	return "the approval mode is " + string(mode)
}

// approvalModeNames renders the three modes for a usage line, in the order
// internal/config offers them, so a reader told the choices here sees the same
// order /approve itself would report back.
func approvalModeNames() string {
	names := make([]string, 0, len(config.ApprovalModes))
	for _, m := range config.ApprovalModes {
		names = append(names, string(m))
	}
	return strings.Join(names, ", ")
}

// permissionGrant is one directory's allowed programs, as /permission holds it.
//
// It is a plain struct rather than a bare map entry, so the directory it was granted
// for travels with the list of programs when /permission reports what is held, and so
// a future consumer (see the dispatcher's permissions field, and the gap this records)
// reads one shape rather than reconstructing it from two parallel maps.
type permissionGrant struct {
	programs map[string]bool
}

// permission is the handler for `/permission`.
//
// # What this is, and the gap it leaves open
//
// The command table's summary is "grant or refuse programs in a directory", and the
// design behind it (adr-0000032's unimplemented permissions store, in the project's
// own design discussion) is a persistent, per-directory, per-program grant list that
// something - the shell tool, the git tool, or the approval check ask.go runs before
// either - consults before it refuses a call under ApprovalDeny or asks under
// ApprovalAsk.
//
// Nothing in internal/tools reads a per-directory or per-program list today.
// internal/tools.Shell and internal/tools.Git each carry one session-wide Approval
// mode (see internal/tools/approval.go: ask, allow, or deny, and nothing finer), and
// internal/tools.Shell's own allowlist (shellPermitted in shell.go) is a fixed table
// compiled into the binary, not a list a reader can add a program to. Building the
// full store this command's summary implies - a consulted, persisted,
// per-directory grant - would mean deciding where runTool reads it from, whether a
// grant widens what Shell's fixed allowlist already permits or only narrows
// ApprovalAllow's blanket yes, and whether it survives a restart, none of which this
// task can settle without guessing at a design nobody has written down yet.
//
// So this is deliberately the smaller half: an in-memory grant list, held on the
// dispatcher and gone when the process exits, that `/permission add` and
// `/permission remove` write to and that `/permission` with no argument reports.
// Nothing reads it yet. A model asking for a program in a directory this list grants
// is still settled by the session's one approval mode and Shell's fixed allowlist,
// exactly as it was before this command existed. This is the flagged v1 scoping gap:
// /permission records intent, and wiring it into runTool's decision is further work.
func (d *dispatcher) permission(args string) (tui.Result, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return tui.Result{Text: d.permissionReport()}, nil
	}

	action, rest, _ := strings.Cut(args, " ")
	switch action {
	case "add":
		return d.permissionAdd(strings.TrimSpace(rest))
	case "remove":
		return d.permissionRemove(strings.TrimSpace(rest))
	default:
		return tui.Result{}, fmt.Errorf("/permission %s is not a sub-command; use add or remove", action)
	}
}

// permissionFields splits "DIR PROG..." into a directory and one or more programs,
// refusing either half that is missing. A grant with no program named is not a
// grant at all, and a program named with no directory has nothing to be scoped to.
func permissionFields(rest string) (dir string, programs []string, err error) {
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return "", nil, fmt.Errorf("want DIR PROG...; a directory and at least one program are both required")
	}
	return fields[0], fields[1:], nil
}

// permissionAdd grants one or more programs in a directory.
func (d *dispatcher) permissionAdd(rest string) (tui.Result, error) {
	dir, programs, err := permissionFields(rest)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/permission add %w", err)
	}

	if d.permissions == nil {
		d.permissions = make(map[string]*permissionGrant)
	}
	g, ok := d.permissions[dir]
	if !ok {
		g = &permissionGrant{programs: make(map[string]bool)}
		d.permissions[dir] = g
	}
	for _, p := range programs {
		g.programs[p] = true
	}

	return tui.Result{Text: fmt.Sprintf("%s may now run %s in %s (not yet consulted anywhere; see /permission's own doc comment)",
		strings.Join(programs, ", "), pluralPrograms(len(programs)), dir)}, nil
}

// permissionRemove refuses one or more programs in a directory that were granted.
//
// Removing the last program a directory held leaves no entry for it rather than an
// entry with an empty program set, so a later `/permission` with no argument does
// not report a directory that grants nothing.
func (d *dispatcher) permissionRemove(rest string) (tui.Result, error) {
	dir, programs, err := permissionFields(rest)
	if err != nil {
		return tui.Result{}, fmt.Errorf("/permission remove %w", err)
	}

	g, ok := d.permissions[dir]
	if !ok {
		return tui.Result{Text: fmt.Sprintf("%s has no grants, so there is nothing to remove", dir)}, nil
	}
	for _, p := range programs {
		delete(g.programs, p)
	}
	if len(g.programs) == 0 {
		delete(d.permissions, dir)
	}

	return tui.Result{Text: fmt.Sprintf("%s may no longer run %s in %s",
		strings.Join(programs, ", "), pluralPrograms(len(programs)), dir)}, nil
}

// pluralPrograms renders "program" or "programs" to match a count, for a message
// that reads naturally whether one name or several were given.
func pluralPrograms(n int) string {
	if n == 1 {
		return "program"
	}
	return "programs"
}

// permissionReport is what `/permission` with no argument prints: every directory
// holding a grant, and the programs granted in it, sorted so two runs of the same
// state read identically.
func (d *dispatcher) permissionReport() string {
	if len(d.permissions) == 0 {
		return "no permissions are granted; this in-memory list is not yet consulted by any tool (see /permission's own doc comment)"
	}

	dirs := make([]string, 0, len(d.permissions))
	for dir := range d.permissions {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var b strings.Builder
	b.WriteString("permissions granted this session (not yet consulted by any tool):\n")
	for _, dir := range dirs {
		g := d.permissions[dir]
		programs := make([]string, 0, len(g.programs))
		for p := range g.programs {
			programs = append(programs, p)
		}
		sort.Strings(programs)
		fmt.Fprintf(&b, "  %s: %s\n", dir, strings.Join(programs, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// tools is the handler for `/tools`.
//
// It reports exactly what a turn is actually offered: each tool's Name() and its
// Describe().Function.Description, the same two things ask.go's own capabilities
// function writes into the system message every turn sends (see ask.go, the "Tools:"
// loop in capabilities), plus the working directory every one of them is contained
// to. Nothing here restates what a tool does in different words, for the reason
// capabilities' own doc comment gives: a second description is a second thing that
// goes stale the day the tool's own one changes and this one does not.
//
// The toolset is built fresh by newToolset(dir) rather than read off the dispatcher,
// because the dispatcher does not hold the one ask() builds: ask() constructs its
// own toolset once per interface, closed over inside the closure openInterface hands
// to the session loop, and the dispatcher is a separate object with no reach into
// that closure. Calling newToolset(dir) a second time is exactly what ask.go itself
// does to build the schemas a turn is offered, so /tools reports the same list a
// turn would get by asking the same question ask.go asks, not a guess at it.
func (d *dispatcher) tools() (tui.Result, error) {
	if d.session == nil {
		return tui.Result{}, fmt.Errorf("/tools needs an open interface")
	}

	dir := d.session.Options().WorkingDir
	notionToken, err := d.cfg.NotionToken()
	if err != nil {
		return tui.Result{}, err
	}
	toolset := newToolset(dir, notionToken)

	if len(toolset) == 0 {
		return tui.Result{Text: fmt.Sprintf("no tools are available, contained to %s", dir)}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d tools, contained to %s:\n", len(toolset), dir)
	for _, t := range toolset {
		fmt.Fprintf(&b, "  %s: %s\n", t.Name(), t.Describe().Function.Description)
	}
	return tui.Result{Text: strings.TrimRight(b.String(), "\n")}, nil
}
