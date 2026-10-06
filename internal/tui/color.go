package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// RGB is a colour in three components.
//
// It is a value rather than a byte triplet so a colour can be passed around and
// compared without a caller knowing the order, and so a theme and a palette role
// can be the same type rather than two that need converting between.
type RGB struct {
	R, G, B uint8
}

// RGBFromHex reads a `#rrggbb` value.
//
// The form is refused rather than trimmed: a theme names a colour in a document
// rather than at a prompt, and a value with spaces in it is a value that was
// assembled rather than written. The empty string is not an error, since a theme
// is allowed to name no colour on one side.
func RGBFromHex(s string) (RGB, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return RGB{}, false
	}
	if !strings.HasPrefix(s, "#") || len(s) != 7 {
		return RGB{}, false
	}

	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return RGB{}, false
	}
	return RGB{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n)}, true
}

// Hex renders a colour the way a theme would write it.
func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// Sequence renders the colour as a direct-RGB escape.
//
// The direct-RGB form rather than a 256-colour index, and that is a reversal of a
// documented decision rather than an exception to it: the design records that
// Terminal.app support was the reason for the 256 mapping and that the reason did
// not survive, so an authored colour passes through as written rather than landing
// at the nearest cube cell.
func (c RGB) Sequence() string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B)
}

// BackgroundSequence renders the colour as a background rather than a foreground.
func (c RGB) BackgroundSequence() string {
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.R, c.G, c.B)
}

// TCellColor renders the colour for tcell, which draws by cell and style rather than
// by escape sequence.
//
// It carries the same direct-RGB value Sequence and BackgroundSequence write, so a
// role reads the same whichever renderer is asking: the three are three callers of
// one set of values, not three decisions about what a role looks like.
func (c RGB) TCellColor() tcell.Color {
	return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
}

// Theme is what a reader named, as opposed to what the palette decided.
//
// It is a separate type rather than two colours on the Palette, since the question
// "what did the reader ask for" and the question "which table should I read" are
// different questions and a struct holding both invites an answer that conflates
// them.
type Theme struct {
	// Name is what the reader called it, for a diagnostic. It is not used to
	// select anything: a theme is a name in a file and the values are what
	// matter, so two names with the same values select the same table.
	Name string

	// Foreground and Background are as written, empty where the theme named
	// none. An empty foreground means the terminal theme is followed, and an
	// empty background is a background that cannot be judged, which falls
	// through to dark.
	Foreground string
	Background string

	// foregroundRGB and backgroundRGB are the parsed values, nil where the theme
	// named none or named something unparseable. They are held apart from the
	// strings so the caller is not asked to parse the same value twice and so a
	// value that cannot be read is distinguishable from one that was never
	// written.
	foregroundRGB, backgroundRGB *RGB
}

// NewTheme reads a theme from the values a file or a command gave.
//
// An unparseable colour is an error rather than a silent absence. A reader who
// wrote `#gggggg` and got the terminal theme back would conclude colour is off
// rather than that the value is wrong, and the difference between the two is the
// difference between fixing a character and turning a setting off.
func NewTheme(name, foreground, background string) (Theme, error) {
	t := Theme{Name: name, Foreground: foreground, Background: background}

	if foreground != "" {
		rgb, ok := RGBFromHex(foreground)
		if !ok {
			return Theme{}, fmt.Errorf("tui: theme %q has a foreground of %q, which is not #rrggbb",
				name, foreground)
		}
		t.foregroundRGB = &rgb
	}

	if background != "" {
		rgb, ok := RGBFromHex(background)
		if !ok {
			return Theme{}, fmt.Errorf("tui: theme %q has a background of %q, which is not #rrggbb",
				name, background)
		}
		t.backgroundRGB = &rgb
	}

	return t, nil
}

// ForegroundRGB returns the theme's foreground, and whether it named one.
func (t *Theme) ForegroundRGB() (RGB, bool) {
	if t == nil || t.foregroundRGB == nil {
		return RGB{}, false
	}
	return *t.foregroundRGB, true
}

// BackgroundRGB returns the theme's background, and whether it named one.
func (t *Theme) BackgroundRGB() (RGB, bool) {
	if t == nil || t.backgroundRGB == nil {
		return RGB{}, false
	}
	return *t.backgroundRGB, true
}

// ErrNoColor reports a call that cannot be made because colour is off.
//
// It is an error rather than an empty sequence so a caller that forgets to check
// gets a refusal instead of writing a row with no colour in it, which reads as a
// role that was decided to be plain.
var ErrNoColor = errors.New("tui: colour is off, so there is nothing to write")
