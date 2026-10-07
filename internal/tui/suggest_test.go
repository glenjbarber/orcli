package tui

import (
	"reflect"
	"testing"
)

// TestLevenshteinKnownDistances checks the primitive in isolation, against cases a
// reader can verify by hand, so it is confirmed correct independent of the
// "did you mean" feature built on top of it.
func TestLevenshteinKnownDistances(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want int
	}{
		{"identical strings", "quit", "quit", 0},
		{"both empty", "", "", 0},
		{"one empty", "", "quit", 4},
		{"the other empty", "quit", "", 4},
		{"one substitution", "quit", "quot", 1},
		{"one insertion", "new", "news", 1},
		{"one deletion", "news", "new", 1},
		{"two substitutions", "qiot", "quit", 2},
		{"completely different strings", "abc", "xyz", 3},
		// Levenshtein distance is symmetric; this checks the implementation
		// does not accidentally favour one argument order over the other.
		{"symmetric", "quot", "quit", 1},
	}

	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("%s: levenshtein(%q, %q) = %d, want %d", c.name, c.a, c.b, got, c.want)
		}
		if got := levenshtein(c.b, c.a); got != c.want {
			t.Errorf("%s (reversed): levenshtein(%q, %q) = %d, want %d", c.name, c.b, c.a, got, c.want)
		}
	}
}

// TestSuggestFindsACloseTypo covers the worked example from the feature request
// directly against the real command table: /qiot does not share a prefix with
// /quit, so Complete's prefix matching could never have found it, but it is within
// two edits (two substitutions) of it, which is this function's threshold for a
// four-letter typed name.
func TestSuggestFindsACloseTypo(t *testing.T) {
	got := Suggest("qiot")
	want := []string{"quit"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Suggest(%q) = %v, want %v", "qiot", got, want)
	}
}

// TestSuggestFindsNothingTooFarAway covers the threshold's other side: a name with
// no real command anywhere near it gets no guess at all, rather than the closest
// name in the table regardless of how far away that still is.
func TestSuggestFindsNothingTooFarAway(t *testing.T) {
	if got := Suggest("nonesuch"); len(got) != 0 {
		t.Errorf("Suggest(%q) = %v, want no suggestions", "nonesuch", got)
	}
}

// TestSuggestScalesTheThresholdWithLength covers the reasoning on suggestThreshold's
// doc comment: a flat distance-2 threshold would let a three-letter name match
// another three-letter name that is a different command entirely. "key" and "new"
// are both real, three-letter commands two edits apart; a reader who typed one
// should not be told it might have meant the other.
func TestSuggestScalesTheThresholdWithLength(t *testing.T) {
	if d := levenshtein("key", "new"); d != 2 {
		t.Fatalf("this test assumes key and new are 2 edits apart, got %d", d)
	}

	if got := Suggest("key"); len(got) != 0 {
		t.Errorf("Suggest(%q) = %v, want no suggestions (key and new are too far apart at this length)", "key", got)
	}

	// A single-letter slip on the same short name is still close enough: "ket" is
	// one substitution from "key".
	got := Suggest("ket")
	want := []string{"key"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Suggest(%q) = %v, want %v", "ket", got, want)
	}
}

// TestSuggestIncludesHiddenAliasesButNamesTheListedCommand covers the hidden-alias
// decision from the PR: a typo close to a hidden name (/cognito, kept as a
// compatibility spelling for /stealth) still gets a hint, because the reader
// mistyped a real command this build answers to even if that spelling is not the one
// /help lists. What is suggested is the listed name, /stealth, not the hidden
// spelling itself - a hidden name is accepted when typed exactly, not recommended as
// the thing to type.
func TestSuggestIncludesHiddenAliasesButNamesTheListedCommand(t *testing.T) {
	got := Suggest("cognitoo")
	want := []string{"stealth"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Suggest(%q) = %v, want %v", "cognitoo", got, want)
	}
}

// TestSuggestBreaksTiesAlphabetically covers the "what happens on a tie" decision:
// when more than one real name sits at the same closest distance, every one of them
// is returned (up to the cap), sorted so the same typo always produces the same
// hint regardless of map iteration order.
func TestSuggestBreaksTiesAlphabetically(t *testing.T) {
	got := Suggest("zzzz")
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("Suggest(%q) = %v is not sorted", "zzzz", got)
			break
		}
	}
}
