package tui

import (
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestSetAliasSavesANameThatInvokesACommand covers the ordinary case, including the one
// that prompted the command: a model shortcut carrying an argument, which an alias
// holding only a command name could not be.
func TestSetAliasSavesANameThatInvokesACommand(t *testing.T) {
	if err := SetAlias("dots", "model", "studio-test/does-not-exist-test"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}

	invokes, args, saved, found := Resolve("dots")
	if !found {
		t.Fatal("a saved name did not resolve")
	}
	if !saved {
		t.Error("the name resolved but was not reported as saved")
	}
	if invokes != "model" {
		t.Errorf("it invokes %q, want model", invokes)
	}
	if args != "studio-test/does-not-exist-test" {
		t.Errorf("its argument is %q, want the model named", args)
	}
}

// TestSetAliasRefusesACommandName is the whole of what this command is careful about. A
// saved name shadowing a built-in would make /model mean something the help does not
// say it means.
func TestSetAliasRefusesACommandName(t *testing.T) {
	err := SetAlias("model", "models", "")
	if err == nil {
		t.Fatal("a saved name was allowed to take a command's name")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Errorf("the refusal is %q, want it to name what was refused", err)
	}
	if _, _, saved, _ := Resolve("model"); saved {
		t.Error("the refused name resolved as a saved one")
	}
}

// TestSetAliasRefusesAHiddenName covers the other half of what a name may not take. A
// hidden name is a spelling the table accepts but does not advertise, and a saved name
// taking one would advertise it.
func TestSetAliasRefusesAHiddenName(t *testing.T) {
	hidden := hiddenNames()
	if len(hidden) == 0 {
		t.Skip("the table carries no hidden names, so there is nothing to collide with")
	}

	if err := SetAlias(hidden[0], "color", ""); err == nil {
		t.Errorf("a saved name was allowed to take the hidden name %q", hidden[0])
	}
	if _, _, _, found := Resolve(hidden[0]); !found {
		t.Errorf("%q stopped resolving, so the refusal changed the table", hidden[0])
	}
}

// TestSetAliasReplacesANameTheReaderMade covers the ordinary case of changing your own
// shortcut, which is not a mistake and should not be refused as though it were.
func TestSetAliasReplacesANameTheReaderMade(t *testing.T) {
	if err := SetAlias("replace-me", "model", "first/model"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}
	if err := SetAlias("replace-me", "model", "second/model"); err != nil {
		t.Fatalf("SetAlias again: %v", err)
	}

	if _, args, _, _ := Resolve("replace-me"); args != "second/model" {
		t.Errorf("the argument is %q, want the second one", args)
	}
}

// TestSetAliasRefusesNamesItCannotHold covers the shapes a name may not take. A name
// with a space cannot be looked up as one word, and a name with a slash would be read as
// a path rather than as a name.
func TestSetAliasRefusesNamesItCannotHold(t *testing.T) {
	for _, name := range []string{"", "  ", "two words", "/slashed", "with/slash"} {
		if err := SetAlias(name, "help", ""); err == nil {
			t.Errorf("the name %q was accepted", name)
		}
	}
}

// TestSetAliasNeedsACommand covers the case where a reader typed the name and forgot the
// rest. A saved name that invokes nothing is a name that fails at the moment it is used,
// which is worse than being refused now.
func TestSetAliasNeedsACommand(t *testing.T) {
	if err := SetAlias("orphan", "", ""); err == nil {
		t.Fatal("a saved name with no command was accepted")
	}
	if err := SetAlias("orphan", "   ", ""); err == nil {
		t.Fatal("a saved name whose command is only whitespace was accepted")
	}
}

// TestSetAliasKeepsANameForACommandThisBuildLacks covers a saved name standing for a
// command the table does not carry.
//
// The alias is kept rather than refused, since the command may arrive in a later build
// and the reader named it on purpose. The test is on the reporting: Resolve says the
// name is saved and names the command it wants, so a caller can report an absent command
// rather than running a name it cannot resolve.
func TestSetAliasKeepsANameForACommandThisBuildLacks(t *testing.T) {
	if err := SetAlias("future", "a-command-not-yet", ""); err != nil {
		t.Fatalf("SetAlias refused a name for a command this build lacks: %v", err)
	}

	invokes, _, saved, found := Resolve("future")
	if !found || !saved {
		t.Fatal("a saved name for an absent command stopped resolving")
	}
	if invokes != "a-command-not-yet" {
		t.Errorf("it invokes %q, want the name it was given", invokes)
	}
	if _, ok := AliasSummary("future"); !ok {
		t.Error("it has no help line, so a reader cannot see what it runs")
	}
}

// TestRemoveAliasReportsWhetherThereWasOne covers the two cases separately, because a
// reader clearing out shortcuts they no longer use should not be refused for having
// already removed one.
func TestRemoveAliasReportsWhetherThereWasOne(t *testing.T) {
	if err := SetAlias("temporary", "help", ""); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}

	if !RemoveAlias("temporary") {
		t.Error("removing a saved name reported that there was none")
	}
	if RemoveAlias("temporary") {
		t.Error("removing it a second time reported that there was one")
	}
	if _, _, _, found := Resolve("temporary"); found {
		t.Error("a removed name still resolves")
	}
}

// TestResolveFindsCommandsAsWellAsSavedNames covers the property a dispatcher depends
// on: one lookup answers for both kinds of name.
func TestResolveFindsCommandsAsWellAsSavedNames(t *testing.T) {
	if err := SetAlias("resolve-both", "model", "some/model"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}

	cases := []struct {
		name    string
		invokes string
		saved   bool
	}{
		{"model", "model", false},
		{"help", "help", false},
		{"resolve-both", "model", true},
	}

	for _, c := range cases {
		invokes, _, saved, found := Resolve(c.name)
		if !found {
			t.Errorf("%q did not resolve", c.name)
			continue
		}
		if invokes != c.invokes {
			t.Errorf("%q invokes %q, want %q", c.name, invokes, c.invokes)
		}
		if saved != c.saved {
			t.Errorf("%q reported saved=%v, want %v", c.name, saved, c.saved)
		}
	}
}

// TestResolveReportsAnAbsentNameAsAbsent rather than as an empty command. A dispatcher
// asking about a name it does not know needs to say so, and an answer of "" is
// indistinguishable from an alias of nothing.
func TestResolveReportsAnAbsentNameAsAbsent(t *testing.T) {
	invokes, args, saved, found := Resolve("no-such-name")
	if found {
		t.Error("an absent name reported that it was found")
	}
	if invokes != "" || args != "" || saved {
		t.Errorf("an absent name returned %q, %q, %v", invokes, args, saved)
	}
}

// TestIsSavedAliasSeparatesTheTwoKinds covers the distinction the completer needs: a
// saved name renders with its own wording rather than with a summary.
func TestIsSavedAliasSeparatesTheTwoKinds(t *testing.T) {
	if err := SetAlias("kind-check", "model", ""); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}

	if !IsSavedAlias("kind-check") {
		t.Error("a saved name is not reported as one")
	}
	if IsSavedAlias("model") {
		t.Error("a command is reported as a saved name")
	}
}

// TestAliasNamesAreOrdered covers the order a completer cycles through. It is
// alphabetical rather than the order they were made, since the order a reader happened
// to type first is an accident rather than a sequence.
//
// The test sorts what it inserted and compares against the same prefix of the list, so
// it does not depend on what other tests in this binary have saved.
func TestAliasNamesAreOrdered(t *testing.T) {
	inserted := []string{"ordered-zulu", "ordered-alpha", "ordered-mike"}
	for _, name := range inserted {
		if err := SetAlias(name, "help", ""); err != nil {
			t.Fatalf("SetAlias %q: %v", name, err)
		}
	}

	all := AliasNames()
	if !sort.StringsAreSorted(all) {
		t.Fatalf("AliasNames is not sorted: %v", all)
	}

	got := orderedSubset(all, "ordered-")
	want := []string{"ordered-alpha", "ordered-mike", "ordered-zulu"}
	if len(got) != len(want) {
		t.Fatalf("the ordered names are %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("the ordered names are %v, want %v", got, want)
		}
	}
}

// orderedSubset returns the names carrying a prefix, in the order they were given.
func orderedSubset(all []string, prefix string) []string {
	var out []string
	for _, name := range all {
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
	}
	return out
}

// TestAliasSummarySaysWhatItRuns covers the help line. A saved name has no summary of its
// own and should not carry a sentence nobody wrote about a reader's own shortcut.
func TestAliasSummarySaysWhatItRuns(t *testing.T) {
	if err := SetAlias("summary-plain", "help", ""); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}
	if err := SetAlias("summary-arg", "model", "some/model"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}

	if got, ok := AliasSummary("summary-plain"); !ok || got != "run /help" {
		t.Errorf("the summary is %q, %v, want run /help", got, ok)
	}
	if got, ok := AliasSummary("summary-arg"); !ok || got != "run /model some/model" {
		t.Errorf("the summary is %q, %v, want the command and its argument", got, ok)
	}
	if _, ok := AliasSummary("model"); ok {
		t.Error("a command was given a summary for an alias")
	}
}

// TestSavedNamesAndCommandsDoNotCollide is the invariant the whole store rests on:
// SetAlias refuses to create a name the table already answers to, so the two kinds can
// never both exist and Resolve never has to choose.
func TestSavedNamesAndCommandsDoNotCollide(t *testing.T) {
	for _, c := range commands {
		if err := SetAlias(c.Name, "help", ""); err == nil {
			t.Errorf("a saved name was allowed to take the command %q", c.Name)
		}
	}
	if err := SetAlias("beside", "help", ""); err != nil {
		t.Fatalf("a saved name beside the commands was refused: %v", err)
	}
}

// TestConcurrentSetAndResolve covers the store under the race detector, since a
// dispatcher resolving a name while a reader saves another is the ordinary case once two
// workers are running.
func TestConcurrentSetAndResolve(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := range 100 {
			if err := SetAlias(concurrentName(i), "help", ""); err != nil {
				t.Errorf("SetAlias: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			Resolve("model")
			AliasNames()
			IsSavedAlias("model")
		}
	}()

	wg.Wait()
}

// concurrentName builds a distinct name for the concurrency test.
func concurrentName(i int) string {
	return "concurrent" + string(rune('a'+i%26))
}

// hiddenNames returns every hidden name in the table.
//
// It is a function here rather than one in the command file so that the test can ask the
// question without restating how the table is built.
func hiddenNames() []string {
	var out []string
	for _, c := range commands {
		out = append(out, c.Hidden...)
	}
	return out
}
