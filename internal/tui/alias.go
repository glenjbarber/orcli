package tui

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// userAlias is a name a reader made that invokes a command.
//
// It is a value rather than a Command so that a saved name can never be mistaken for
// one of the built-in's own: an alias has no summary of its own, it has the name it
// stands for, and every path that renders help or offers completion has to decide what
// to do with it. Making it a Command would mean a second Command with an empty Summary
// flowing through the same renderers, and a help line with nothing in it is a help line
// that has to be filtered rather than a case that cannot arise.
type userAlias struct {
	// name is the name the reader typed, without the leading slash.
	name string

	// invokes is the command name the alias runs.
	//
	// It is the name rather than a *Command, so an alias recorded against a command
	// that a later build removes resolves to nothing and is reported, rather than
	// holding a pointer into a table that has been rebuilt underneath it.
	invokes string

	// args is the rest of the line, carried with the alias.
	//
	// The example that prompted this is `/alias dots model studio-test/model`, where
	// `model` is the command and the rest its argument. An alias that could only stand
	// for a bare command name would have no way to be a model shortcut, which is the
	// case worth having.
	args string
}

// aliasStore holds the names a reader made.
//
// It is a map rather than a slice because the two questions are different: a dispatcher
// needs to know whether a name is a saved one and what it runs, and that is a lookup.
// Listing them in order is what AliasNames needs, and it sorts what it gets.
type aliasStore struct {
	mu sync.RWMutex
	by map[string]userAlias
}

// aliases is the session's saved names.
//
// It is package-level rather than a field on a Session because the command table is
// package-level, and a saved name has to survive the same way a built-in does: a reader
// who typed `/alias dots model x` in one turn and `/dots` in the next expects the same
// answer, and a store hung off a session value that a caller forgets to carry would
// answer differently for reasons no reader could see.
var aliases = &aliasStore{by: map[string]userAlias{}}

// AliasNames returns the saved names, in order.
//
// The order is alphabetical for the reason Names orders the built-ins: a completer
// that cycles through names in the order they were made is a completer whose order is
// an accident of what the reader happened to type first.
func AliasNames() []string {
	aliases.mu.RLock()
	defer aliases.mu.RUnlock()

	out := make([]string, 0, len(aliases.by))
	for name := range aliases.by {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// SetAlias saves a name, and reports a refusal by name rather than returning a bare
// false.
//
// A reader who typed `/alias model x` needs to be told that `model` is a command, not
// that the command failed. The refusal carries the name and the reason, and the caller
// puts it in a row.
//
// A name that is already a saved one is replaced rather than refused, since changing
// what a name you made yourself means is the ordinary case rather than a mistake. A name
// the table already answers to is refused, and that refusal is the whole of what this
// command is careful about: the dispatcher looks a typed name up in one place, and a
// saved name shadowing a built-in would make `/model` mean something different from
// what the help says it means.
//
// One lookup covers the built-in names, their aliases and their hidden names, since
// the table puts all three in the same map. A hidden name is a spelling the table
// accepts but does not advertise, and a saved name taking one would advertise it.
func SetAlias(name, invokes, args string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("an alias needs a name, and none was given")
	}
	if strings.ContainsAny(name, " \t/") {
		return fmt.Errorf("an alias name is one word without a slash, and %q is not", name)
	}
	if _, taken := Lookup(name); taken {
		return fmt.Errorf("%q is already a command or one of its names, and a saved name cannot be one", name)
	}

	if strings.TrimSpace(invokes) == "" {
		return fmt.Errorf("an alias needs a command to invoke, and none was given")
	}

	aliases.mu.Lock()
	defer aliases.mu.Unlock()
	aliases.by[name] = userAlias{name: name, invokes: invokes, args: strings.TrimSpace(args)}
	return nil
}

// RemoveAlias drops a saved name, and reports whether there was one.
//
// A name that is not there is not an error. A reader clearing out shortcuts they no
// longer use should not have to remember which ones they had already removed, and a
// command that failed on a name already gone is a command that gets typed twice.
func RemoveAlias(name string) bool {
	aliases.mu.Lock()
	defer aliases.mu.Unlock()

	if _, found := aliases.by[name]; !found {
		return false
	}
	delete(aliases.by, name)
	return true
}

// Resolve reports the command a typed name answers to, saved names included.
//
// It is the one lookup a dispatcher should make, because it is the one that knows about
// both kinds of name. The two cannot collide: SetAlias refuses a name the table already
// answers to, so Resolve never has to choose between them.
func Resolve(name string) (invokes, args string, saved, found bool) {
	aliases.mu.RLock()
	a, isSaved := aliases.by[name]
	aliases.mu.RUnlock()

	if isSaved {
		return a.invokes, a.args, true, true
	}

	c, isCommand := Lookup(name)
	if !isCommand {
		return "", "", false, false
	}
	return c.Name, "", false, true
}

// IsSavedAlias reports whether a typed name is one the reader made.
//
// The completer needs it separately from Resolve, because a saved name is offered with
// the commands and rendered with its own wording: what it runs, not a summary nobody
// wrote.
func IsSavedAlias(name string) bool {
	aliases.mu.RLock()
	defer aliases.mu.RUnlock()
	_, found := aliases.by[name]
	return found
}

// AliasSummary is the help line for a saved name.
//
// It says what the name runs rather than carrying a summary of its own, since a saved
// name has no description and the reader needs to know the command, not to read a
// sentence nobody wrote about their own shortcut.
func AliasSummary(name string) (string, bool) {
	aliases.mu.RLock()
	a, found := aliases.by[name]
	aliases.mu.RUnlock()

	if !found {
		return "", false
	}
	if a.args == "" {
		return "run /" + a.invokes, true
	}
	return "run /" + a.invokes + " " + a.args, true
}
