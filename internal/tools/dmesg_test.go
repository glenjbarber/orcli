package tools

import (
	"strings"
	"testing"
)

// TestDmesgTakesNoPath covers the shape that dmesg needs.
//
// Every argument dmesg accepts is either an option or a value belonging to one: a
// facility to filter on, a level, a column count, a follow mode, or a buffer. A
// facility reads as an ordinary word, so any other shape resolves it as a file name and
// refuses the call. That is the defect noPaths exists to prevent, and dmesg is the case
// where it is easiest to get wrong.
func TestDmesgTakesNoPath(t *testing.T) {
	shell := NewShell(tree(t))

	arguments := []string{
		"kern",        // a facility, which is a word and not a file
		"err",         // another facility
		"3",           // a level, which looks like a number and is not a path
		"--level=err", // the long spelling of the same
		"--since=1h",  // a duration with a unit
		"util",        // another facility
	}

	out, err := shell.checkArgs("dmesg", arguments)
	if err != nil {
		t.Fatalf("dmesg with facility arguments: %v", err)
	}

	for i, want := range arguments {
		if out[i] != want {
			t.Errorf("argument %d became %q, want it unchanged as %q", i, out[i], want)
		}
	}
}

// TestDmesgRefusesAPathLeavingTheTree is not the right shape of test and this is
// why: dmesg takes no path, so there is nothing for a path to escape through. The
// program reads the kernel ring buffer and writes to the terminal, so a model naming
// a file with it is naming an argument the program will not read rather than reaching
// outside the tree.
func TestDmesgDoesNotRefuseAPathLikeArgument(t *testing.T) {
	shell := NewShell(tree(t))

	for _, arg := range []string{"/etc/passwd", "../outside", "kernel.log"} {
		out, err := shell.checkArgs("dmesg", []string{arg})
		if err != nil {
			t.Errorf("dmesg %q was refused: %v", arg, err)
			continue
		}
		if out[0] != arg {
			t.Errorf("dmesg %q became %q, want it unchanged", arg, out[0])
		}
	}
}

// TestDmesgMayNotBeGivenTheClearOption is the whole reason dmesg needs an entry in
// the refused-option table rather than only one in the list.
//
// -c clears the ring buffer and -r reads it and then clears it. A model asked why a
// driver failed to load and told to run dmesg would destroy the evidence with the
// first of those, and nothing afterwards can bring it back. Both the short and long
// spellings are named, since a program that accepts one accepts a different option and
// this list names options rather than prefixes.
func TestDmesgMayNotBeGivenTheClearOption(t *testing.T) {
	shell := NewShell(tree(t))

	refused := []string{"-c", "--clear", "-r", "--read-clear", "-C"}
	for _, option := range refused {
		t.Run(option, func(t *testing.T) {
			if _, err := shell.checkArgs("dmesg", []string{option}); err == nil {
				t.Errorf("dmesg %s was accepted, want it refused", option)
			}
		})
	}
}

// TestDmesgMayBeGivenTheReadOptions covers the other side, so the refusal list cannot
// grow by accident until every option is refused.
//
// These are the options a model asks for when it wants to read the buffer rather than
// destroy it, and a list that refused them would push a model toward -c.
func TestDmesgMayBeGivenTheReadOptions(t *testing.T) {
	shell := NewShell(tree(t))

	allowed := [][]string{
		{"--human"},
		{"--notime"},
		{"--color=never"},
		{"-T"},
		{"-t"},
		{"-x"},
		{"--reverse"},
		{"--follow"},
		{"-f"},
		{"--kernel"},
		{"--force"},
	}

	for _, args := range allowed {
		if _, err := shell.checkArgs("dmesg", args); err != nil {
			t.Errorf("dmesg %v was refused: %v", args, err)
		}
	}
}

// TestDmesgCarriesNoMarkers covers the marker table from the other side: a program on
// the list with no entry passes through, and dmesg needs none because it interprets no
// argument as a command and reads no file. It is the assertion that would fail if a
// future change added dmesg to the marker table by mistake.
func TestDmesgCarriesNoMarkers(t *testing.T) {
	if _, ok := programMarkers["dmesg"]; ok {
		t.Error("dmesg has markers: it interprets no argument and takes no path, " +
			"so a marker there refuses a facility name for no reason")
	}
}

// TestDmesgIsNamedInTheDescription holds the schema to the list, since a model that
// is not shown the name will not ask for it and a model shown a name it cannot run
// learns only that the tool lies.
func TestDmesgIsNamedInTheDescription(t *testing.T) {
	description := NewShell(t.TempDir()).Describe().Function.Description

	if !strings.Contains(description, "dmesg") {
		t.Error("dmesg is permitted but is not named in the description")
	}
	if !strings.Contains(description, "dmesg may not be given -c") {
		t.Error("the description does not say that dmesg may not be given -c")
	}
}
