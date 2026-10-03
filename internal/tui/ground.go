package tui

import (
	"fmt"
	"strings"
)

// Ground is which of the two role tables to read.
type Ground int

const (
	// GroundAuto judges by the background the theme names, and dark when it names
	// none.
	GroundAuto Ground = iota
	// GroundDark is the dark table, whatever the theme says.
	GroundDark
	// GroundLight is the light table, whatever the theme says.
	GroundLight
)

// String renders a ground for a setting or a diagnostic.
func (g Ground) String() string {
	switch g {
	case GroundDark:
		return "dark"
	case GroundLight:
		return "light"
	default:
		return "auto"
	}
}

// ParseGround reads the three grounds and refuses anything else.
//
// A reader who typed a word and got the default back would conclude the command
// ignored them, so an unrecognised ground is an error rather than a fallback to
// auto. Auto is spelled out rather than defaulted to, since auto is a choice and
// silence is not.
func ParseGround(s string) (Ground, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto":
		return GroundAuto, nil
	case "dark":
		return GroundDark, nil
	case "light":
		return GroundLight, nil
	default:
		return GroundAuto, fmt.Errorf("tui: %q is not a ground: it is auto, dark or light", s)
	}
}

// lightBackgroundLuminance is the figure a background is judged against.
//
// It is the relative-luminance midpoint rather than an average of the channels.
// The eye is not equally sensitive to each channel, so an average calls a
// saturated blue brighter than a mid grey when it is not, and a theme whose
// background is that blue would be judged a dark theme when it is a light one.
const lightBackgroundLuminance = 0.5

// groundIsLight reports which table to read.
//
// The order is deliberate and each step is a reason. An explicit choice from
// `/theme` beats a file, since a reader who has said which ground they are on has
// said it more recently than a file did. Otherwise a background the theme names is
// judged by its luminance. Otherwise dark, which is the common case.
//
// A background the client cannot read is not a background it judges against, so an
// absent or unresolvable one falls through to dark rather than to a guess.
func groundIsLight(chosen Ground, theme *Theme) bool {
	switch chosen {
	case GroundDark:
		return false
	case GroundLight:
		return true
	}

	if theme == nil {
		return false
	}
	rgb, ok := theme.BackgroundRGB()
	if !ok {
		return false
	}
	return relativeLuminance(rgb) >= lightBackgroundLuminance
}

// relativeLuminance returns how bright a colour is, from 0 to 1.
//
// Each channel is linearised first, since a terminal's channel values are not
// perceptually spaced and the weights mean nothing applied to raw ones. The weights
// are the standard ones: the eye is far more sensitive to green than to blue, and a
// figure that treated them alike would call a saturated blue brighter than a mid
// grey when it is not.
func relativeLuminance(rgb RGB) float64 {
	return 0.2126*linearise(rgb.R) + 0.7152*linearise(rgb.G) + 0.0722*linearise(rgb.B)
}

// linearise turns one channel from its stored value into a linear one.
func linearise(c uint8) float64 {
	v := float64(c) / 255
	if v <= 0.03928 {
		return v / 12.92
	}
	f := (v + 0.055) / 1.055
	return f * f * f
}
