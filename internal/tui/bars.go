package tui

import (
	"strings"
)

// barWidth is the narrowest a status bar is ever drawn.
//
// One, and that is deliberate rather than a figure waiting to be tuned. The width is a
// floor and not a cap: a bar is drawn at the width of the fields it carries, it never
// shrinks below the floor, and it never drops a field. The frame rows are written without
// being clipped, so a bar wider than the terminal overflows its row rather than losing the
// end of itself.
//
// So the fields that do not fit are not drawn as a shorter bar. They are carried, and what a
// narrow reader loses is a field rather than half of one, since a cut field looks like a
// value the reader mistyped.
const barWidth = 1

// RenderTop renders the top bar, the one nearer the log.
//
// The field order is the order of loss: the fields that change second by second are the
// ones that survive a narrow bar longest, and Status is last to go for exactly that
// reason. Context, In, Out, Cost and Credits are carried by nothing yet, so they render
// as a dash until a caller has a figure for them.
//
// The bar is drawn at the width of its fields rather than to the terminal's width, so it
// occupies the same columns on every terminal and a reader who knows where to look finds
// it in the same place.
func RenderTop(status, reasoning, contextUsed, in, out, cost, credits string) string {
	return RenderBar([]Field{
		{Name: "Status", Value: status},
		{Name: "Reasoning", Value: reasoning},
		{Name: "Context", Value: contextUsed},
		{Name: "In", Value: in},
		{Name: "Out", Value: out},
		{Name: "Cost", Value: cost},
		{Name: "Credits", Value: credits},
	}, barWidth)
}

// RenderBottom renders the bottom bar, the one nearer the bottom of the screen.
//
// The three leading entries are not settings. Session names which session the field is
// typed into, and the two bracketed words name the two ways the terminal is taking the
// reader's input. They lead because they are what a reader looks for when they cannot
// work out why a key did what it did: which session they are in, and whether the mouse is
// being taken.
//
// The mode entries are deliberately not names and values. A `Mouse: on` field reads as a
// preference a reader could be trying to set and `[Mouse]` reads as a thing that is
// happening now. That is the whole of the difference between this bar and the one above it,
// which is entirely figures that change.
//
// The order of loss is Verbosity, Approval, Model, Provider, and the three leading entries
// are never dropped: a bar that loses the session it is in has stopped saying which bar it
// is.
func RenderBottom(session string, mouse, copy bool,
	provider, model, verbosity, approval string) string {
	parts := make([]string, 0, 7)
	parts = append(parts, session)
	if mouse {
		parts = append(parts, "[Mouse]")
	}
	if copy {
		parts = append(parts, "[Copy]")
	}
	parts = append(parts,
		"Provider: "+orNone(provider),
		"Model: "+orNone(model),
		"Approval: "+orNone(approval),
		"Verbosity: "+orNone(verbosity),
	)
	return joinWithin(parts, " | ", barWidth)
}

// RenderProvider renders the bottom bar from the figures a session reports.
//
// It is kept as a name the caller already uses, and it renders the bottom bar rather than
// the bar the merged frame called Provider: the bar nearest the bottom of the screen is the
// one that names the provider, and the field order on it is the one the reader settled.
func RenderProvider(session string, mouse, copy bool,
	provider, model, verbosity, approval string) string {
	return RenderBottom(session, mouse, copy, provider, model, verbosity, approval)
}

// RenderStatus renders the top bar from the figures a session reports.
//
// The state carries the detail as a note beside it rather than as a fifth field, since a
// bar that grows a comma is a bar no reader can scan.
func RenderStatus(status, reasoning, contextUsed, in, out, cost, credits string) string {
	return RenderTop(status, reasoning, contextUsed, in, out, cost, credits)
}

// detailSuffix renders the note beside a state.
//
// It is a parenthesised note rather than a fifth field, since a bar that grows a comma is a
// bar no reader can scan, and a state spelled `paused, buffered 12` is a state rather than a
// state and a note.
func detailSuffix(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

// orNone renders an empty string as a dash.
//
// A field with no value is a dash rather than nothing, so the bar holds its shape as values
// arrive and a reader scanning it is not reading a row whose columns move.
func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// indent puts a row at the field's left edge.
//
// It is a prefix rather than padding on the caller so the field, the figure and the two
// status rows cannot drift apart: a reader looking at the bottom of the screen is following
// one column, and four rows at four different left edges is a frame that makes them look
// unrelated.
func indent(s string) string {
	if s == "" {
		return ""
	}
	return strings.Repeat(" ", FieldIndent) + s
}
