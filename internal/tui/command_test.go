package tui

import (
	"strings"
	"testing"
)

// TestCompletionAppendsASpace is the requirement itself: a name the reader has typed
// is completed by adding a space, and the caret lands after it.
//
// The space is what separates a finished name from the argument that follows. A
// completion that leaves the caret against the last character of the name makes the
// reader correct it before they can type anything, and it makes a name with an
// argument indistinguishable from the end of the name.
func TestCompletionAppendsASpace(t *testing.T) {
	text, caret, whole := Completion("/col")
	if !whole {
		t.Fatal("col gave no whole completion, want one")
	}
	if want := "/color "; text != want {
		t.Errorf("completion gave %q, want %q", text, want)
	}
	if want := len("/color "); caret != want {
		t.Errorf("the caret is at %d, want %d: after the space, not before it", caret, want)
	}
}

// TestCompletionAcceptsTheSlashAndTheBareName covers the two spellings a caller has,
// since the dispatcher strips the slash before it asks and the completer is also
// called on a bare prefix.
func TestCompletionAcceptsTheSlashAndTheBareName(t *testing.T) {
	for _, prefix := range []string{"/colo", "colo"} {
		names, whole := Complete(prefix)
		if !whole || len(names) != 1 || names[0] != "color" {
			t.Errorf("Complete(%q) gave %q whole=%v, want color", prefix, names, whole)
		}
	}
}

func TestAtPluginTriggerKeepsTheFreeFormTask(t *testing.T) {
	for _, line := range []struct {
		input string
		args  string
	}{{"@notion", ""}, {"@notion find a page", "find a page"}} {
		name, args, ok := IsCommand(line.input)
		if !ok || name != "@notion" || args != line.args {
			t.Errorf("IsCommand(%q) = %q, %q, %v", line.input, name, args, ok)
		}
	}
}

// TestCompletionOnAWholeNameIsWhole is the case the space exists for: the name is
// already typed, so the only thing missing is the separator.
func TestCompletionOnAWholeNameIsWhole(t *testing.T) {
	names, whole := Complete("/color")
	if !whole {
		t.Error("a name that is already whole was not reported as whole")
	}
	if len(names) != 1 || names[0] != "color" {
		t.Errorf("Complete gave %q, want [color]", names)
	}

	if text, caret, _ := Completion("/color"); text != "/color " || caret != len("/color ") {
		t.Errorf("Completion gave %q caret %d, want the name and a space", text, caret)
	}
}

// TestCompletionIsAmbiguousWhenItCannotBeSettled covers the case where a prefix is
// several names. There is no single right completion, and the caller cycles rather
// than guessing, since guessing is how a completer changes what the reader meant.
func TestCompletionIsAmbiguousWhenItCannotBeSettled(t *testing.T) {
	for _, prefix := range []string{"/m", "/mo", "/c"} {
		names, whole := Complete(prefix)
		if len(names) < 2 {
			t.Errorf("Complete(%q) gave %d names, want several", prefix, len(names))
			continue
		}
		if whole {
			t.Errorf("Complete(%q) claimed a whole completion from %d names", prefix, len(names))
		}
		if text, _, _ := Completion(prefix); text != prefix {
			t.Errorf("an ambiguous prefix was changed to %q, want it left as typed", text)
		}
	}
}

// TestCompletionOnNothingMatchesChangesNothing covers a prefix that matches nothing.
// Rewriting it to something else would replace what the reader typed with a guess
// about what they meant.
func TestCompletionOnNothingMatchesNothing(t *testing.T) {
	text, caret, whole := Completion("/nosuch")
	if text != "/nosuch" {
		t.Errorf("Completion gave %q, want the text unchanged", text)
	}
	if caret != len("/nosuch") {
		t.Errorf("the caret is at %d, want it at the end of what was typed", caret)
	}
	if whole {
		t.Error("a prefix matching nothing was reported as a whole completion")
	}
}

// TestCompletionOnAnEmptyPrefixOffersEverything covers the reader who presses Tab on
// an empty field, which is how a reader finds out what exists.
func TestCompletionOnAnEmptyPrefixOffersEverything(t *testing.T) {
	names, whole := Complete("")

	if len(names) == 0 {
		t.Fatal("an empty prefix offered nothing")
	}
	if whole {
		t.Error("an empty prefix was reported as a whole completion")
	}
	if len(names) != len(Names()) {
		t.Errorf("an empty prefix offered %d names, want %d", len(names), len(Names()))
	}
}

// TestNamesAreSorted covers the order. A completer that cycles in declaration order
// cycles in whatever order the source file happens to be in, which is an accident
// rather than a decision.
func TestNamesAreSorted(t *testing.T) {
	names := Names()
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("names are out of order at %d: %q then %q", i, names[i-1], names[i])
		}
	}
}

// TestNamesExcludeHidden covers what hidden means. A name accepted when typed and
// never offered is a compatibility spelling rather than a feature.
func TestNamesExcludeHidden(t *testing.T) {
	for _, name := range Names() {
		if name == "colour" {
			t.Error("a hidden alias was offered by the completer")
		}
	}
}

// TestHiddenNamesAreStillAccepted covers the other half of hidden. A name the
// completer refuses to offer is still a name the dispatcher answers to, or a reader
// who used it before it was hidden is told they typed it wrong.
func TestHiddenNamesAreStillAccepted(t *testing.T) {
	c, found := Lookup("colour")
	if !found {
		t.Fatal("a hidden name was not accepted by the lookup")
	}
	if c == nil || c.Name != "color" {
		t.Errorf("a hidden name resolved to %v, want the command it stands for", c)
	}
}

// TestLookupFindsEveryName covers the dispatcher path: every visible name, every
// alias and every hidden name is a key, so a name the table knows is a name the table
// can find.
func TestLookupFindsEveryName(t *testing.T) {
	for _, name := range Names() {
		c, found := Lookup(name)
		if !found {
			t.Errorf("Lookup(%q) found nothing, and Names offers it", name)
			continue
		}
		if c == nil {
			t.Errorf("Lookup(%q) returned no command and no answer", name)
		}
	}
	if _, found := Lookup("nosuch"); found {
		t.Error("Lookup found a command that is not in the table")
	}
}

// TestLookupRefusesASlash covers the caller's contract. The dispatcher strips the
// slash, and a caller that passed one has a bug that stripping would hide.
func TestLookupRefusesASlash(t *testing.T) {
	if _, found := Lookup("/color"); found {
		t.Error("Lookup accepted a name with a slash")
	}
}

// TestEveryCommandHasASummary covers the help, which renders the summary beside the
// name. A command with an empty summary is a line of help with nothing on it, which
// is a line the reader reads past.
func TestEveryCommandHasASummary(t *testing.T) {
	for _, name := range Names() {
		c, found := Lookup(name)
		if !found {
			t.Fatalf("Lookup(%q) found nothing", name)
		}
		if strings.TrimSpace(c.Summary) == "" {
			t.Errorf("%s has no summary, so the help would carry an empty line", name)
		}
	}
}

// TestEveryAliasResolvesToItsCommand covers a table that could answer to a name it
// does not describe. An alias pointing at another command is a name two commands
// answer to, and which one wins would be the order of the source file.
func TestEveryAliasResolvesToItsCommand(t *testing.T) {
	for _, c := range commands {
		for _, a := range c.Aliases {
			got, found := Lookup(a)
			if !found {
				t.Errorf("the alias %q of %s resolves to nothing", a, c.Name)
				continue
			}
			if got.Name != c.Name {
				t.Errorf("the alias %q of %s resolves to %s", a, c.Name, got.Name)
			}
		}
	}
}

// TestNoNameIsHeldTwice covers the other direction. A name two commands answer to is
// a name the reader types and gets one of them, chosen by nothing they could see.
func TestNoNameIsHeldTwice(t *testing.T) {
	seen := map[string]string{}
	for _, c := range commands {
		for _, name := range append([]string{c.Name}, append(c.Aliases, c.Hidden...)...) {
			if held, dup := seen[name]; dup {
				t.Errorf("%s is held by both %s and %s", name, held, c.Name)
				continue
			}
			seen[name] = c.Name
		}
	}
}

// TestIdleOnlyIsCarriedByTheTable checks the commands that are refused while a turn
// works, since the question is asked from the table rather than by each command.
func TestIdleOnlyIsCarriedByTheTable(t *testing.T) {
	refused := map[string]bool{
		"new": true, "load": false, "clear": false, "btw": false,
		"main": true, "compact": true, "approve": true,
	}

	for name, want := range refused {
		c, found := Lookup(name)
		if !found {
			t.Errorf("Lookup(%q) found nothing", name)
			continue
		}
		if c.IdleOnly != want {
			t.Errorf("%s has IdleOnly=%v, want %v", name, c.IdleOnly, want)
		}
	}
}

// TestArgsAreNamedForTheCommandsThatTakeThem checks that a command whose completer
// should offer an argument says what the argument is, and one that takes none says
// nothing. A command offering an argument it does not take produces a completion the
// reader cannot use.
func TestArgsAreNamedForTheCommandsThatTakeThem(t *testing.T) {
	withArgs := map[string]bool{
		"search": true, "models": true, "model": true, "copy": true,
		"verbosity": true, "help": false, "quit": false, "clear": false,
	}

	for name, want := range withArgs {
		c, found := Lookup(name)
		if !found {
			t.Errorf("Lookup(%q) found nothing", name)
			continue
		}
		if got := c.Args != ""; got != want {
			t.Errorf("%s has Args=%q, want an argument: %v", name, c.Args, want)
		}
	}
}

// TestCompletionLeavesAWordWithNoSlashAlone covers the ordinary case, since most of
// what a reader types is not a command. Completing prose into a command name would be
// the completer guessing at ordinary text.
func TestCompletionLeavesAWordWithNoSlashAlone(t *testing.T) {
	text, _, whole := Completion("what does the log do")
	if text != "what does the log do" {
		t.Errorf("ordinary text became %q", text)
	}
	if whole {
		t.Error("ordinary text was reported as a whole completion")
	}
}

// TestCompletingAWholeNameFromItsFirstLetter ends on the thing a reader actually
// does: a prefix that settles on one name gets that name and a space, and a prefix
// that settles on several is left alone so the caller can cycle.
func TestCompletingAWholeNameFromItsFirstLetter(t *testing.T) {
	if names, whole := Complete("/mo"); whole || len(names) < 2 {
		t.Errorf("Complete(/mo) gave %d names whole=%v, want several and not whole",
			len(names), whole)
	}

	if text, caret, _ := Completion("/quit"); text != "/quit " || caret != len("/quit ") {
		t.Errorf("Completion(/quit) gave %q caret %d, want the name and a space", text, caret)
	}
}

// TestLevelIsInTheTableAlongsideVerbosity covers the command this unit adds: it
// should read the way /verbose and /verbosity already do, an argument-taking name
// the completer and the help both know about.
func TestLevelIsInTheTableAlongsideVerbosity(t *testing.T) {
	c, found := Lookup("level")
	if !found {
		t.Fatal("/level is not in the table")
	}
	if c.Args == "" {
		t.Error("/level has no Args, so the completer cannot offer a preset name")
	}
	if strings.TrimSpace(c.Summary) == "" {
		t.Error("/level has no summary")
	}
}

// TestTheTableIsFilledInInitRatherThanAVariableInitialiser is the constraint behind
// the shape of this file.
//
// The help renderer reaches this table, so a variable initialiser reaching the help
// would form an initialisation cycle and Go would refuse to compile. The lookup is in
// the same function for the other reason: a package-level initialiser runs before
// `init`, so a lookup written as one would be built from an empty table and every
// lookup would miss.
func TestTheTableIsFilledInInitRatherThanAVariableInitialiser(t *testing.T) {
	if len(commands) == 0 {
		t.Fatal("the table is empty: init did not run, or ran before this")
	}
	if byName == nil {
		t.Error("the lookup is nil: init did not run, or ran before this")
	}
	if _, found := Lookup("color"); !found {
		t.Error("the lookup is empty though the table is not: it was built before init")
	}
}
