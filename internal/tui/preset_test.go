package tui

import (
	"strings"
	"testing"
)

// TestEveryPresetIsDistinctAndDocumented covers the table's own shape: a preset
// with no style is not bundling anything, two presets with the same verbosity and
// style are the same preset twice, and a name that repeats is a name /level cannot
// resolve to one answer.
func TestEveryPresetIsDistinctAndDocumented(t *testing.T) {
	seenName := map[string]bool{}
	seenEffect := map[string]string{}

	for _, p := range presets {
		if p.Name == "" {
			t.Error("a preset has no name")
		}
		if p.Summary == "" {
			t.Errorf("%s has no summary", p.Name)
		}
		if p.Style == "" {
			t.Errorf("%s has no style, so it only sets a number and bundles nothing", p.Name)
		}
		if p.Verbosity < 0 || p.Verbosity > 5 {
			t.Errorf("%s has Verbosity %d, want 0-5", p.Name, p.Verbosity)
		}

		if seenName[p.Name] {
			t.Errorf("%s is in the table twice", p.Name)
		}
		seenName[p.Name] = true

		effect := p.Style
		if other, dup := seenEffect[effect]; dup {
			t.Errorf("%s and %s have the same style, so they are not distinct", p.Name, other)
		}
		seenEffect[effect] = p.Name
	}
}

// TestDirectMatchesGlensExampleClosely covers the one preset the Notion todo names
// explicitly: terse, low verbosity, at most one caveat per reply.
func TestDirectMatchesGlensExampleClosely(t *testing.T) {
	p, found := LookupPreset("direct")
	if !found {
		t.Fatal("there is no \"direct\" preset")
	}
	if p.Verbosity > 1 {
		t.Errorf("direct has Verbosity %d, want it low", p.Verbosity)
	}
	lower := strings.ToLower(p.Style)
	if !strings.Contains(lower, "two sentence") {
		t.Errorf("direct's style is %q, want it to name two sentences", p.Style)
	}
	if !strings.Contains(lower, "one caveat") {
		t.Errorf("direct's style is %q, want it to cap caveats at one", p.Style)
	}
}

// TestLookupPresetRefusesAnUnknownName covers the typo /level has to report by name.
func TestLookupPresetRefusesAnUnknownName(t *testing.T) {
	if _, found := LookupPreset("nosuch"); found {
		t.Error("LookupPreset found a name that is not in the table")
	}
}

// TestPresetNamesAreSorted covers the listing /level's refusal and report use.
func TestPresetNamesAreSorted(t *testing.T) {
	names := PresetNames()
	if len(names) != len(presets) {
		t.Fatalf("PresetNames gave %d names, want %d", len(names), len(presets))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("names are out of order at %d: %q then %q", i, names[i-1], names[i])
		}
	}
}
