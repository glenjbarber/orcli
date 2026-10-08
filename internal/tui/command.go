package tui

import (
	"sort"
	"strings"
)

// Command is one entry in the table.
//
// The fields answer three questions a command has to answer once rather than three
// times: what the dispatcher calls it, what the help says about it, and what the
// completer offers after it.
type Command struct {
	// Name is the name the reader types, without the leading slash.
	//
	// It is stored without the slash so a lookup is a map access on what the reader
	// typed after the slash, and so the completer and the help agree on the spelling
	// without either of them re-adding it.
	Name string

	// Summary is the one line the help renders beside it.
	//
	// It is a field rather than prose composed at the help, because a help line
	// composed from the name and the flags would be a second description that can
	// disagree with the first.
	Summary string

	// Aliases are the other names the dispatcher answers to.
	//
	// An alias is offered by the completer on the same terms as the name it stands
	// for, since an alias a reader cannot discover is an alias only the author knows.
	Aliases []string

	// Hidden names are accepted when typed and never listed, never completed, and
	// never in the help.
	//
	// It is how `/colour` is a name for `/color` that the reader who used it already
	// knows and the reader who did not need to see. A hidden alias is a compatibility
	// surface rather than a feature, and listing it would spend a line of help on a
	// spelling nobody asked for.
	Hidden []string

	// Args names the argument each form takes, for the completer to offer after the
	// name.
	//
	// It is a description rather than a grammar. There is no argument grammar in this
	// program yet, and building one to complete against would be building the harder
	// half of the problem first.
	Args string

	// IdleOnly means the command is refused while a turn is working.
	//
	// It is a field rather than a check in each command, since the question is asked
	// before any of them runs and a command that answered it for itself would answer
	// it in a place the dispatcher does not look.
	IdleOnly bool
}

// commands is the table, and byName is the lookup built from it.
//
// Both are filled in `init`, and that is a constraint rather than a style. The help
// renderer reaches this table, so a variable initialiser reaching the help would form
// an initialisation cycle, which Go reports at compile time. The two are filled in the
// same function for a second reason: a package-level initialiser runs before `init`, so
// a `byName` written as a variable initialiser would be built from an empty table and
// every lookup would miss. Both facts are worth writing down, since the second produces
// a table that is silently empty rather than a build failure.
var (
	commands []Command
	byName   map[string]*Command
)

func init() {
	commands = []Command{
		{
			Name:    "help",
			Summary: "list this table",
		},
		{
			Name:    "version",
			Summary: "print the version",
		},
		{
			// The connection is automatic and the probe is the reader's own first
			// question, so there is no command that tests it. An entry with no body
			// is one a reader types and is refused, which reads worse than a name
			// that is not there at all.
			Name:    "key",
			Summary: "report the usage against the key",
		},
		{
			Name:    "search",
			Args:    "TEXT",
			Summary: "search the log, filtered as it is typed",
		},
		{
			Name:    "models",
			Args:    "TEXT",
			Summary: "list the models, filtered as it is typed",
		},
		{
			Name:    "freemodels",
			Args:    "TEXT",
			Summary: "list the models that cost nothing, filtered as typed",
		},
		{
			Name:    "model",
			Args:    "NAME",
			Summary: "show or choose the model, without an argument to list",
		},
		attributeCommand,
		{
			Name:     "new",
			Summary:  "clear the conversation",
			IdleOnly: true,
		},
		{
			Name:    "bell",
			Summary: "ring the terminal bell on reply, on or off",
		},
		{
			Name:    "color",
			Summary: "turn color on or off, and save the choice",
			Hidden:  []string{"colour"},
		},
		{
			// Renamed from /cognito, confirmed by Glen on 2026-10-07; /cognito is
			// kept as a hidden alias on the same grounds /colour is kept for
			// /color, above.
			Name:    "stealth",
			Summary: "record nothing, on or off",
			Hidden:  []string{"cognito"},
		},
		{
			Name:    "verbosity",
			Args:    "0-5",
			Summary: "how much the model is asked to answer with",
		},
		{
			Name:    "verbose",
			Summary: "report the shape of each streamed turn, on or off",
		},
		{
			Name:    "level",
			Args:    "NAME",
			Summary: "set a bundled reply style (direct, concise, thorough, casual), without an argument to report",
		},
		{
			Name:    "delegate",
			Args:    "QUESTION",
			Summary: "ask a question alongside, without recording it",
		},
		{
			Name:    "pane",
			Args:    "main|delegate|spawn",
			Summary: "show the conversation, the delegate output or the spawn output",
		},
		{
			Name:    "spawn",
			Args:    "QUESTION",
			Summary: "answer a question in a worker given the tools",
		},
		{
			Name:    "btw",
			Args:    "QUESTION",
			Summary: "start a thread branched from this conversation",
		},
		{
			Name:    "close",
			Args:    "N",
			Summary: "dereference the level a copy would name",
		},
		{
			Name:    "queue",
			Args:    "TEXT",
			Summary: "add a follow-up prompt to the queue",
		},
		{
			Name:    "redirect",
			Summary: "interrupt and redirect the prompt",
		},
		{
			Name:     "main",
			Summary:  "leave the thread and return to the conversation",
			IdleOnly: true,
		},
		{
			Name:     "compact",
			Summary:  "summarise the conversation and start again",
			IdleOnly: true,
		},
		{
			Name:    "save",
			Args:    "NAME",
			Summary: "write the conversation to a file of its own",
		},
		{
			Name:    "load",
			Args:    "NAME",
			Summary: "resume a conversation saved with /name",
		},
		{
			Name:    "begin",
			Args:    "NOTE",
			Summary: "fork a pane and hand the new one a note describing a task",
		},
		{
			Name:    "mouse",
			Summary: "turn mouse reporting on or off, for wheel scrolling",
		},
		{
			Name:    "copymode",
			Summary: "freeze the footer and twiddle sweep for a terminal-native copy",
		},
		{
			Name:    "pause",
			Summary: "stop what the client writes, and toggle the mouse",
		},
		{
			Name:    "clear",
			Summary: "clear the log",
		},
		{
			Name:    "trace",
			Args:    "on|off|status",
			Summary: "capture the redacted conversation stream for debugging",
		},
		{
			Name:    "info",
			Summary: "report the session settings",
		},
		{
			Name:    "copy",
			Args:    "N",
			Summary: "copy the level N, or the whole conversation",
		},
		{
			Name:    "permission",
			Args:    "add|remove DIR PROG...",
			Summary: "grant or refuse programs in a directory",
		},
		{
			Name:    "autosave",
			Args:    "on|off|now",
			Summary: "write the conversation without being asked",
		},
		{
			Name:     "approve",
			Args:     "ask|allow|refuse",
			Summary:  "report or set whether programs run without asking",
			IdleOnly: true,
		},
		{
			Name:    "tools",
			Summary: "list the tools the model is given, and the root they are in",
		},
		testCommand,
		{
			Name:    "quit",
			Summary: "leave the interface",
		},
		{
			Name:    "exit",
			Summary: "leave the interface",
		},
		cloudflareCommand,
	}

	byName = make(map[string]*Command, len(commands))
	for i := range commands {
		c := &commands[i]
		byName[c.Name] = c
		for _, a := range c.Aliases {
			byName[a] = c
		}
		for _, h := range c.Hidden {
			byName[h] = c
		}
	}
}

// testCommand is the `/test` entry in the table.
//
// It is a development command that lists a directory the way ls does and writes the
// listing where a reply from the model is written, so a reader can fill the log with rows
// whose number and length they chose and watch where the frame puts them.
//
// It carries no credential, reaches no network, and asks the model nothing, which is what
// makes it useful for a frame test: the rows it produces are the same every run, so what
// moves on screen moved because of the drawing and not because of a model.
var testCommand = Command{
	Name:    "test",
	Args:    "DIR",
	Summary: "list a directory, for testing how the frame draws",
}

// TestCommand returns the `/test` entry.
//
// It is a function rather than a field read from the table for the reason
// AttributeCommand has: the entry is built in its own file and registered in command.go's
// init, and this says which value is meant without making a caller depend on a table a
// reader can be looking at.
func TestCommand() Command { return testCommand }

// Lookup returns the command a typed name answers to, and whether it does.
//
// The name arrives without the slash. A name with one is refused rather than
// stripped, since the dispatcher strips the slash before it asks and a caller that
// passed one has a bug in it that stripping would hide.
func Lookup(name string) (*Command, bool) {
	c, found := byName[name]
	return c, found
}

// isHidden reports whether a name is one of a command's hidden names.
func isHidden(c *Command, name string) bool {
	for _, h := range c.Hidden {
		if h == name {
			return true
		}
	}
	return false
}

// Names returns every name the completer offers, in order.
//
// Hidden names are not in it, since a name that is never listed and never completed
// is what hidden means. The order is alphabetical rather than the order of the
// table, because a completer that cycles through names in the order they were
// declared is a completer whose order is an accident of the source file.
func Names() []string {
	out := make([]string, 0, len(byName))
	for name, c := range byName {
		if !isHidden(c, name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Complete offers the names that carry a typed prefix, and whether the prefix settles
// on one whole name.
//
// The second answer is what lets the caller do the thing a completer exists for. A
// prefix that is one whole name is completed by appending a space and nothing else,
// which is what lets a reader type `/color ` and then an argument rather than pressing
// Tab before every word. A prefix that is several names has no single completion, and
// the caller cycles through them.
//
// The prefix may carry a leading slash or not. The dispatcher strips the slash before
// it looks a name up, and a completer that answered one spelling and not the other
// would be a completer that works from one call site and not the next.
func Complete(prefix string) (names []string, whole bool) {
	bare := strings.TrimPrefix(prefix, "/")

	candidates := Names()
	if bare == "" {
		return candidates, false
	}

	matches := make([]string, 0, len(candidates))
	for _, name := range candidates {
		if strings.HasPrefix(name, bare) {
			matches = append(matches, name)
		}
	}

	// One match means the prefix settles on a single name, whether or not the prefix
	// is that name already. Both are the same thing to the caller: there is one
	// completion and it is the rest of the word, followed by a space.
	if len(matches) == 1 {
		return matches, true
	}

	return matches, false
}

// Plugins returns the built-in plugin names accepted by the @plugin command form.
func Plugins() []string { return []string{"apiary", "cloudflare", "github", "notion"} }

// PluginSubcommands returns the discoverable operations for a built-in plugin.
func PluginSubcommands(name string) []string {
	switch name {
	case "apiary":
		return []string{"query"}
	case "cloudflare":
		return []string{"confirm", "dns"}
	case "github":
		return []string{"tasks"}
	case "notion":
		return []string{"api", "blocks", "comments", "data", "fetch", "pages", "search", "tasks"}
	default:
		return nil
	}
}

// CompleteAt returns matches for the token at the caret, using preceding text to
// select command, plugin, or subcommand candidates.
func CompleteAt(before, word string) (matches []string, whole bool) {
	if before == "" {
		if strings.HasPrefix(word, "/") {
			matches, whole := Complete(word)
			for i := range matches {
				matches[i] = "/" + matches[i]
			}
			return matches, whole
		}
		if strings.HasPrefix(word, "@") {
			matches, whole := matching(Plugins(), strings.TrimPrefix(word, "@"))
			for i := range matches {
				matches[i] = "@" + matches[i]
			}
			return matches, whole
		}
		return Complete(word)
	}
	context := strings.TrimSpace(before)
	if strings.HasPrefix(context, "/") && !strings.Contains(context, " ") {
		name := strings.TrimPrefix(context, "/")
		choices := []string{"help"}
		if c, ok := Lookup(name); ok {
			choices = append(choices, argumentChoices(c.Args)...)
		}
		return matching(choices, word)
	}
	if strings.HasPrefix(context, "@") && !strings.Contains(context, " ") {
		name := strings.TrimPrefix(context, "@")
		if len(PluginSubcommands(name)) == 0 {
			return nil, false
		}
		return matching(append([]string{"help"}, PluginSubcommands(name)...), word)
	}
	return nil, false
}

func matching(candidates []string, prefix string) ([]string, bool) {
	matches := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			matches = append(matches, candidate)
		}
	}
	return matches, len(matches) == 1
}

func argumentChoices(args string) []string {
	if !strings.Contains(args, "|") {
		return nil
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return nil
	}
	return strings.Split(fields[0], "|")
}

// Completion is what a completer puts in the field, and where the caret goes.
//
// The trailing space is the whole point. It separates a finished name from the
// argument that follows, and without it a name that takes an argument is
// indistinguishable from the end of the name: the reader cannot tell whether to type a
// space or whether the command simply takes nothing after it. One byte, and the
// question the reader was about to ask is already answered.
//
// The slash is put back when the reader typed one. It is part of what was typed and
// part of what the dispatcher will look up, so a completion that dropped it would
// leave a field whose text is not the command the reader is about to run.
//
// It is returned rather than applied. The line editor owns the field and the caret, and
// a completer that wrote into the terminal behind it would be two things writing to one
// place. A caller that cannot use the text and the caret together wants Names and
// Complete instead.
func Completion(prefix string) (text string, caret int, whole bool) {
	names, whole := Complete(prefix)
	if !whole || len(names) == 0 {
		return prefix, len(prefix), false
	}

	slash := ""
	if strings.HasPrefix(prefix, "/") {
		slash = "/"
	}

	text = slash + names[0] + " "
	return text, len(text), true
}

// CompletionAt replaces the token under the caret with one candidate.
func CompletionAt(before, word string) (text string, caret int, whole bool, candidates []string) {
	candidates, whole = CompleteAt(before, word)
	if len(candidates) == 0 {
		return word, len([]rune(word)), false, nil
	}
	if !whole {
		return candidates[0], len([]rune(candidates[0])), false, candidates
	}
	text = candidates[0]
	return text, len([]rune(text)), true, candidates
}
