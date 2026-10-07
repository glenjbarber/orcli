package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Palette turns a role into the bytes that write it.
//
// It is pure: every function takes values and returns strings, and nothing in it
// draws. That is the property that lets the roles be tested without a terminal and
// lets a theme change without a repaint, since a row means the same thing whether
// colour is on or off and the colour is decided here at the last moment before the
// bytes are written.
//
// A row carries a Role rather than a colour, so the screen asks for what a piece of
// text is and this file decides what that looks like. Fifteen roles, and the count
// is fixed: the three tool identities and the three state identities are separate
// roles rather than shades of one, because a reader who cannot tell a file read from
// a command cannot read the log.
type Palette struct {
	// on is whether the reader asked for colour at all. It is a field rather than
	// something read off the sequences, because a theme naming no colour on either
	// side writes no sequences and is still a palette that is on. Those are
	// different states and a caller reading one off the other gets the wrong one.
	on bool

	// ground is which table the roles are read from. Dark is the common case and
	// the one the defaults were chosen for.
	ground Ground

	// fg and bg are the theme sequences written before and after a row. They are
	// empty when the theme named no colour on that side, which is the case a
	// reader following the terminal theme is in.
	fg, bg           string
	fgColor, bgColor tcell.Color
	hasFG, hasBG     bool

	// faint is the alternative for the dim role: the faint attribute rather than a
	// colour. Whether a terminal draws it is unverified, so it is offered rather
	// than defaulted to.
	faint bool

	// paneActive and paneDone are the pane bar's own colours, apart from fg/bg
	// above and from every Role: a pane's state is not a role a row carries, it
	// is a fact about a worker that this palette is told about once per draw.
	// The two hasPaneXxx flags are what let a palette built with neither (every
	// existing caller, and every caller in a test) keep drawing the pane bar in
	// chrome exactly as it did before this existed.
	paneActive, paneDone       RGB
	hasPaneActive, hasPaneDone bool
}

// WithPaneColors returns a palette that colours the pane bar while a worker
// is running, or has just finished, at the pane shown. Either argument may be
// nil, which leaves that one state drawn in chrome.
//
// It is a second method beside NewPalette rather than a parameter on it,
// for the reason WithFaintDim is: these are configured apart from the ground
// and the theme, and a caller that wants the ordinary palette untouched by
// them keeps calling NewPalette alone.
func (p Palette) WithPaneColors(active, done *RGB) Palette {
	if active != nil {
		p.paneActive, p.hasPaneActive = *active, true
	}
	if done != nil {
		p.paneDone, p.hasPaneDone = *done, true
	}
	return p
}

// PaneStyle returns the style the pane bar draws a pane's state in, and
// whether a configured colour applies. state is "running", "done", or
// anything else for neither; the caller falls back to its own chrome style
// when ok is false, which covers colour being off, the state being neither,
// and no colour configured for the state that applies.
func (p Palette) PaneStyle(state string) (style tcell.Style, ok bool) {
	if !p.on {
		return tcell.StyleDefault, false
	}
	switch state {
	case "running":
		if p.hasPaneActive {
			return tcell.StyleDefault.Foreground(p.paneActive.TCellColor()), true
		}
	case "done":
		if p.hasPaneDone {
			return tcell.StyleDefault.Foreground(p.paneDone.TCellColor()), true
		}
	}
	return tcell.StyleDefault, false
}

// NewPalette builds a palette for a ground, a theme and whether colour is on.
//
// The colour flag is a parameter rather than something the palette decides, since the
// configuration file and `/color` decide it and a palette that second-guessed them
// would produce different frames from the same settings.
//
// The ground is settled even with colour off, so a reader who asks which table would be
// read is answered without having to turn colour on.
func NewPalette(on bool, chosen Ground, theme *Theme) Palette {
	p := Palette{on: on, ground: GroundDark}
	if groundIsLight(chosen, theme) {
		p.ground = GroundLight
	}

	if theme != nil {
		if rgb, ok := theme.ForegroundRGB(); ok {
			p.fg = rgb.Sequence()
			p.fgColor = rgb.TCellColor()
			p.hasFG = true
		}
		if rgb, ok := theme.BackgroundRGB(); ok {
			p.bg = rgb.BackgroundSequence()
			p.bgColor = rgb.TCellColor()
			p.hasBG = true
		}
	}
	return p
}

// On reports whether the palette writes colour.
func (p Palette) On() bool { return p.on }

// Ground reports which table the palette reads.
func (p Palette) Ground() Ground { return p.ground }

// WithFaintDim returns a palette whose dim role is the faint attribute rather than a
// colour.
//
// It is an alternative to try rather than a default, since whether a terminal draws
// faint text is unverified. A reader who tries it and cannot see the difference has
// lost nothing but a setting.
func (p Palette) WithFaintDim() Palette {
	p.faint = true
	return p
}

// Sequence returns the bytes that write a role.
//
// An unknown role is chrome rather than an error, since a row with no colour in it is
// readable and a draw that refuses leaves the reader with a blank row. The tables are
// checked by test, so an unknown role here is one the caller invented rather than one
// the tables are missing.
func (p Palette) Sequence(role Role) string {
	if !p.on {
		return ""
	}

	if role == RoleDim && p.faint {
		return faintSequence
	}

	table := RoleTableFor(p.ground)
	rgb, ok := table[role]
	if !ok {
		rgb = table[RoleChrome]
	}
	return rgb.Sequence()
}

// Base returns the sequences written before and after a row.
//
// It is empty on both sides when the terminal theme is followed, which is the case a
// reader with no theme is in. It is a function rather than a field because the reset
// needs it, and a reset followed by a base is not the same sequence as a base.
func (p Palette) Base() string {
	if !p.on {
		return ""
	}
	return p.fg + p.bg
}

// Reset returns the bare reset followed by the base.
//
// A bare reset alone would clear the base and leave the rest of the row in the
// terminal theme rather than on the theme background, which is the one case where
// resetting and restoring are not the same thing.
func (p Palette) Reset() string {
	if !p.on {
		return bareReset
	}
	return bareReset + p.Base()
}

// Style returns the tcell.Style for a role.
//
// It is an alternative to Sequence, which returns an ANSI escape sequence rather
// than a tcell.Style. Both can be used in parallel: callers that want a tcell.Style
// call this, and callers that want an escape sequence call Sequence.
//
// An unknown role returns a style with no foreground set, which reads as the
// terminal's default.
func (p Palette) Style(role Role) tcell.Style {
	if !p.on {
		return tcell.StyleDefault
	}

	table := RoleTableFor(p.ground)
	rgb, ok := table[role]
	if !ok {
		rgb = table[RoleChrome]
	}
	style := tcell.StyleDefault.Foreground(rgb.TCellColor())
	if role == RoleDim && p.faint {
		style = style.Attributes(tcell.AttrDim)
	}
	return style
}

// BaseStyle returns the tcell.Style for the palette's base (foreground and background).
//
// It is an alternative to Base, which returns ANSI escape sequences rather than a
// tcell.Style. Both can be used in parallel: callers that want a tcell.Style call this,
// and callers that want escape sequences call Base.
//
// An empty base (palette off) returns StyleDefault.
func (p Palette) BaseStyle() tcell.Style {
	if !p.on {
		return tcell.StyleDefault
	}
	style := tcell.StyleDefault
	if p.hasFG {
		style = style.Foreground(p.fgColor)
	}
	if p.hasBG {
		style = style.Background(p.bgColor)
	}
	return style
}

// faintSequence is the alternative to a dim colour.
const faintSequence = "\x1b[2m"

// bareReset is the reset with nothing after it.
const bareReset = "\x1b[0m"

// RoleTable is the colour each role is written in.
//
// It is a map rather than an array so that a role with no entry is a missing entry
// rather than a zero written as if it had been chosen. The design requires every role
// to have a value and a test holds the two tables to the same set, so a role added to
// one and not the other is caught rather than rendered as chrome.
type RoleTable map[Role]RGB

// roleSequences is the table for a dark terminal.
//
// The values are fixed rather than ANSI colour numbers, and that is a deliberate
// reversal of an earlier decision: a colour number leaves what the reader sees to the
// terminal theme, and the request was for defaults brighter than the theme usually
// draws them. A reader who wants the old behaviour back accepts the light table or
// names a theme.
//
// The three that must never collide are dim, failure and approval, and the three tool
// identities against those and each other. A reader who cannot tell an approval from a
// failure, or a file read from a command, cannot read the log.
var roleSequences = RoleTable{
	RoleChrome:    {R: 0x9e, G: 0x9e, B: 0x9e},
	RoleNotice:    {R: 0xd7, G: 0xd7, B: 0xaf},
	RoleFailure:   {R: 0xf7, G: 0x62, B: 0x72},
	RoleSuccess:   {R: 0xa6, G: 0xe3, B: 0xa1},
	RoleApproval:  {R: 0xff, G: 0xd7, B: 0x86},
	RoleDim:       {R: 0x74, G: 0x74, B: 0x7d},
	RoleCode:      {R: 0x8f, G: 0xbc, B: 0xbb},
	RoleHeading:   {R: 0xbd, G: 0x93, B: 0xf9},
	RoleEmphasis:  {R: 0xd4, G: 0xd4, B: 0xd4},
	RoleQuote:     {R: 0x94, G: 0xe2, B: 0xb5},
	RoleList:      {R: 0x89, G: 0xb4, B: 0xfa},
	RoleLink:      {R: 0x89, G: 0xdc, B: 0xfe},
	RoleToolFS:    {R: 0xc0, G: 0xca, B: 0xf5},
	RoleToolGit:   {R: 0xfc, G: 0xa9, B: 0xd3},
	RoleToolShell: {R: 0xa6, G: 0xda, B: 0x95},
}

// roleSequencesLight is the table for a light terminal.
//
// It is a second table rather than one table judged at paint time, because the same
// value cannot serve both grounds. A grey that reads as dim on a dark background reads
// as bright on a light one, and a table that knew which ground it was on would need
// the knowledge in every role rather than in one comparison.
//
// The values are darker rather than paler for the same reason: a value chosen to read
// as emphasis on a dark ground is unreadable as emphasis on a light one.
var roleSequencesLight = RoleTable{
	RoleChrome:    {R: 0x58, G: 0x58, B: 0x5e},
	RoleNotice:    {R: 0x35, G: 0x35, B: 0x3d},
	RoleFailure:   {R: 0x9d, G: 0x00, B: 0x06},
	RoleSuccess:   {R: 0x01, G: 0x61, B: 0x00},
	RoleApproval:  {R: 0x8f, G: 0x54, B: 0x00},
	RoleDim:       {R: 0x85, G: 0x87, B: 0x8b},
	RoleCode:      {R: 0x00, G: 0x3a, B: 0x38},
	RoleHeading:   {R: 0x5c, G: 0x1d, B: 0x91},
	RoleEmphasis:  {R: 0x1c, G: 0x1c, B: 0x1c},
	RoleQuote:     {R: 0x00, G: 0x6b, B: 0x2d},
	RoleList:      {R: 0x1c, G: 0x35, B: 0x8e},
	RoleLink:      {R: 0x0a, G: 0x30, B: 0x69},
	RoleToolFS:    {R: 0x3c, G: 0x44, B: 0x7a},
	RoleToolGit:   {R: 0xad, G: 0x00, B: 0x57},
	RoleToolShell: {R: 0x40, G: 0x02, B: 0x50},
}

// roleOrder is every role, in a fixed order.
//
// It exists so the tables are checked against a list rather than against each other.
// Two tables compared only to each other would both be missing the same role and the
// comparison would pass.
var roleOrder = []Role{
	RoleChrome,
	RoleNotice,
	RoleFailure,
	RoleSuccess,
	RoleApproval,
	RoleDim,
	RoleCode,
	RoleHeading,
	RoleEmphasis,
	RoleQuote,
	RoleList,
	RoleLink,
	RoleToolFS,
	RoleToolGit,
	RoleToolShell,
}

// Roles reports every role the palette knows.
func Roles() []Role {
	out := make([]Role, len(roleOrder))
	copy(out, roleOrder)
	return out
}

// RoleTableFor returns the table a ground reads.
//
// It is exported for the tests that hold the two tables to the same set of roles, since
// that check needs to reach both without a caller having to know which is which.
func RoleTableFor(g Ground) RoleTable {
	if g == GroundLight {
		return roleSequencesLight
	}
	return roleSequences
}

// RoleName renders a role for a diagnostic.
//
// It is a name rather than a number because a number in a message tells a reader
// nothing and a name tells them which role is missing.
func RoleName(role Role) string {
	switch role {
	case RoleChrome:
		return "chrome"
	case RoleNotice:
		return "notice"
	case RoleFailure:
		return "failure"
	case RoleSuccess:
		return "success"
	case RoleApproval:
		return "approval"
	case RoleDim:
		return "dim"
	case RoleCode:
		return "code"
	case RoleHeading:
		return "heading"
	case RoleEmphasis:
		return "emphasis"
	case RoleQuote:
		return "quote"
	case RoleList:
		return "list"
	case RoleLink:
		return "link"
	case RoleToolFS:
		return "filesystem"
	case RoleToolGit:
		return "git"
	case RoleToolShell:
		return "shell"
	default:
		return fmt.Sprintf("role(%d)", int(role))
	}
}

// GroundCandidates are the grounds offered by completion.
//
// It is a function rather than a variable so a caller cannot append to the slice and
// change what every other caller offers.
func GroundCandidates() []string { return []string{"auto", "dark", "light"} }

// ThemeNames is the sentence a diagnostic uses to name what a theme asked for, since
// the values are not always readable as values.
func ThemeNames(t Theme) string {
	var parts []string
	if t.Foreground != "" {
		parts = append(parts, "foreground "+t.Foreground)
	}
	if t.Background != "" {
		parts = append(parts, "background "+t.Background)
	}
	if len(parts) == 0 {
		return "the terminal theme on both sides"
	}
	return strings.Join(parts, ", ")
}
