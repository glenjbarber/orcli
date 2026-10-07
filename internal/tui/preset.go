package tui

import "sort"

// Preset is one named interaction style /level can select.
//
// /level bundles several reply characteristics into one name rather than making the
// reader tune verbosity and tone separately every time they want a particular kind
// of answer - the Notion todo's own phrasing, and Glen's own example of what a
// preset should feel like: "be direct, two sentence replies, one caveat."
//
// # v1 scope, decided here rather than left open
//
// Presets are this fixed, named, in-code table, not a reader-editable
// configuration surface. A reader who wants a style this table does not have asks
// for one to be added, the way a reader who wants a new command asks for one,
// rather than editing a file of their own presets. This is a small, reversible
// choice: a later build can turn this table into something the configuration file
// feeds without changing /level's shape.
//
// # What a preset actually changes
//
// Verbosity is a real lever today: Options.Verbosity is read by
// internal/tui/stack.go's fieldVerbosity for the status bar, and a preset moves it.
// Style is also real: cmd/orcli/ask.go's capabilities() reads Session.PresetStyle()
// and appends it to the system message sent with every turn, so it is an
// instruction the model actually receives rather than a label with no effect.
// There is no other tone or reply-shape mechanism in this tree yet - no separate
// system prompt, no per-reply length cap - so a preset's effect is exactly these
// two fields and nothing invented beyond them.
//
// # Default behaviour is unchanged until /level is used
//
// No preset is active until the reader types one. Options.Verbosity starts at zero,
// the same default as today, and Session.Preset() reports "" until /level NAME is
// run: /level is purely opt-in.
//
// # Not the conversation Level
//
// This is a different "level" from the one levels.go and Session.Level(n) name: that
// one is a conversation branch a pane reaches with /copy and /btw, numbered and
// never reused (see DESIGN.md §3, "A pane ID is not a level"). This one is a bundle
// of reply characteristics, named rather than numbered, and the two share an
// English word and nothing else.
type Preset struct {
	// Name is what the reader types after /level, and what /level with no
	// argument reports back.
	Name string

	// Summary is the one line /level and /level NAME report, in the same voice
	// as a command's own Summary in the table in command.go.
	Summary string

	// Verbosity is what Options.Verbosity is set to, 0 to 5 - the same range
	// /verbosity's own Args in command.go names, even though that command has
	// no handler yet.
	Verbosity int

	// Style is the sentence added to the system message a turn sends (see
	// Session.PresetStyle and capabilities() in cmd/orcli/ask.go), naming the
	// characteristics beyond verbosity: tone, caveats, length. It is never
	// empty for a preset in the table below, since a preset that only set a
	// number would not be bundling anything.
	Style string
}

// presets is the fixed table /level chooses from.
//
// Four, not a dozen: DESIGN.md's reader is a sysadmin, a business builder, someone
// in marketing and someone in finance sharing one interface, and a reader choosing
// between a dozen similarly-named styles is a reader who has to read this file to
// pick one. Each is genuinely distinct in both its number and its sentence, and
// "direct" is the one built to match Glen's own example closely: terse,
// low-verbosity, at most one caveat per reply.
var presets = []Preset{
	{
		Name:      "direct",
		Summary:   "terse: about two sentences, at most one caveat",
		Verbosity: 1,
		Style:     "Be direct: answer in about two sentences, and raise at most one caveat.",
	},
	{
		Name:      "concise",
		Summary:   "brief: a short paragraph, trimmed to the question asked",
		Verbosity: 2,
		Style:     "Be concise: answer in a short paragraph, trimmed to what was asked.",
	},
	{
		Name:      "thorough",
		Summary:   "detailed: full explanation, tradeoffs and caveats included",
		Verbosity: 5,
		Style:     "Be thorough: explain fully, including tradeoffs and caveats worth knowing.",
	},
	{
		Name:      "casual",
		Summary:   "conversational tone, moderate length",
		Verbosity: 3,
		Style:     "Write in a relaxed, conversational tone, as you would to a colleague.",
	},
}

// LookupPreset returns the preset a typed name answers to, and whether it does.
func LookupPreset(name string) (Preset, bool) {
	for _, p := range presets {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

// PresetNames returns every preset's name, alphabetically, for /level with a bad
// name to list what it does know and for /level with no argument to point at.
func PresetNames() []string {
	names := make([]string, 0, len(presets))
	for _, p := range presets {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}
