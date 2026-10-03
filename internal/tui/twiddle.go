package tui

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// twiddleFrames is the braille dot-walk, one cell per step of the marquee.
//
// It is the standard walk and the shortest one that reads as rotation rather than
// blinking: every frame moves a different dot, so the figure never repeats a shape inside
// the period. A walk with a repeated frame reads as a stutter at any speed, and a walk
// of four frames reads as a jump rather than a turn.
var twiddleFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// twiddleCells is how many cells the marquee draws.
//
// Two, so the pair reads as one figure turning rather than as two spinners. A braille
// cell is two dots wide, so a pair is a four-dot field, which is about the smallest a
// figure eight fits into without becoming a smudge.
const twiddleCells = 2

// twiddleStep is how long one frame lasts, and the hue turns with it.
//
// Tying the two rates is the decision rather than an accident of the drawing. Left
// unrelated they beat against each other and the figure looks as though it stutters
// rather than turns, because the reader sees the colour change at one rhythm and the shape
// at another.
//
// The figure is eighty milliseconds per step, twice the forty millisecond repaint bound,
// so a repaint lands on every other frame at worst rather than on a random one.
const twiddleStep = 80 * time.Millisecond

// twiddleLightness and twiddleSaturation are fixed while the hue turns.
//
// Fixed because rotating hue alone keeps every channel away from zero. The earlier
// interpolated twiddle moved between the six primaries and had to floor every channel,
// since a blue at zero is not the black that red at zero would be, and the floor put a
// dark spot in the sweep. Holding the lightness and the saturation fixed removes the
// problem rather than papering over it.
const (
	twiddleLightness  = 0.6
	twiddleSaturation = 0.9
)

// Twiddle renders the marquee at a point in its cycle.
//
// It takes the step rather than the time so that the caller decides the clock: the
// session owns when a frame is due, and a function that read the clock itself would be a
// second thing that has to agree with the repaint rate.
//
// The two cells are half a period apart, so the pair reads as one figure with the colour
// turning through it rather than two figures blinking in turn.
func Twiddle(step int) string {
	n := len(twiddleFrames)
	if n == 0 {
		return ""
	}

	i := ((step % n) + n) % n

	var b strings.Builder
	for cell := range twiddleCells {
		// Half a period apart, which for a ten frame walk is five frames.
		b.WriteRune(twiddleFrames[(i+cell*n/2)%n])
	}
	return b.String()
}

// TwiddleHue returns the direct-RGB escape for the twiddle at a step.
//
// The form is `38;2;r;g;b` rather than a 256-colour index. The twiddle is the one thing on
// the frame that needs continuous hue: a rotation through 256 indexed cells is not a
// rotation, it is a walk through a palette, and it bands visibly at every boundary. Two
// columns of output are not worth carrying a whole palette to avoid.
func TwiddleHue(step int) string {
	n := len(twiddleFrames)
	hue := float64(((step%n)+n)%n) * (360 / float64(n))

	r, g, b := hueRGB(hue, twiddleSaturation, twiddleLightness)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

// hueRGB returns the red, green and blue of a hue at a saturation and a lightness.
//
// The conversion is the standard one: the hue picks a point on the colour wheel, the
// saturation decides how far from grey it sits, and the lightness decides how far from
// black or white. Written out rather than taken from a colour library because the only
// caller is a two-character marquee and a dependency for it is a dependency every build
// carries.
func hueRGB(hue, saturation, lightness float64) (int, int, int) {
	hue = math.Mod(math.Mod(hue, 360)+360, 360)

	c := (1 - math.Abs(2*lightness-1)) * saturation
	x := c * (1 - math.Abs(math.Mod(hue/60, 2)-1))
	m := lightness - c/2

	var r, g, b float64
	switch {
	case hue < 60:
		r, g, b = c, x, 0
	case hue < 120:
		r, g, b = x, c, 0
	case hue < 180:
		r, g, b = 0, c, x
	case hue < 240:
		r, g, b = 0, x, c
	case hue < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}

	return toByte(r + m), toByte(g + m), toByte(b + m)
}

// toByte converts a zero to one channel to a byte, clamped.
//
// The clamp is not decoration: a lightness of one and a saturation of one puts the sum
// exactly at one, and a rounding error above it would produce 256, which writes as two
// digits and would shift every row after the twiddle by a column.
func toByte(f float64) int {
	switch {
	case f <= 0:
		return 0
	case f >= 1:
		return 255
	default:
		return int(f*255 + 0.5)
	}
}
