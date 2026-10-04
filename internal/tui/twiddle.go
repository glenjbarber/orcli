package tui

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// The twiddle is one dot tracing a figure eight, and the hue turning with it.
//
// It is not a figure turning in place and it is not two cells: it is a single dot
// walking a lemniscate, so what a reader sees is an infinity sign being drawn over
// and over while its colour turns. The two together are what reads as rotation; the
// path alone reads as a dot moving and the hue alone reads as a colour changing.
//
// # The field
//
// Four dots wide and four dots tall, which is two braille cells side by side. A
// figure eight needs both dimensions to read as a figure eight: in a single cell,
// which is two dots wide, the two loops are narrower than the gap between them and
// the curve collapses into a vertical wiggle.
//
// # Why the curve is scaled on one axis only
//
// The lemniscate of Gerono is x = cos t and y = sin t cos t, and its y reaches only
// half as far as its x. Mapped onto a square field as it stands it uses the middle
// two rows and never reaches the top or the bottom, which is a wave rather than a
// figure eight. So y is doubled before it is mapped, which makes it sin 2t and puts
// the curve across the whole field on both axes.
//
// That is the one adjustment the shape needed, and it is here rather than in the
// mapping because a mapping that has to know which axis needs scaling is a mapping
// that has to be changed every time the field is.

// twiddleRows and twiddleCols are the field the dot walks in.
const (
	twiddleRows = 4
	twiddleCols = 4
)

// twiddleCells is how many braille cells the field spans.
//
// Two, since a braille cell is two dots wide and the field is four. It is named
// because the cell count is what a caller rendering a field knows to count, and a
// field described only in dots has two spellings.
const twiddleCells = (twiddleCols + 1) / 2

// twiddleSamples is how many points are taken around the curve.
//
// More than the number of dots, since consecutive samples landing on the same dot
// are what make the walk look continuous rather than like it is skipping. It is
// taken around a full period rather than a half, so the figure closes on itself.
const twiddleSamples = 64

// twiddleStep is how long one frame lasts, and the hue turns with it.
//
// Tying the two rates is the decision rather than an accident of the drawing. Left
// unrelated they beat against each other and the dot looks as though it stutters
// rather than moves, because the reader sees the colour change at one rhythm and the
// shape at another.
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

// twiddleWalk is the path the dot takes, one entry per step.
//
// It is built rather than written out, since a lemniscate sampled and quantised is
// a few lines of arithmetic and a table of coordinates would be the same walk in a
// form nobody can check by reading it. Consecutive samples landing on the same dot
// are dropped, so a step is a move and the cycle is the moves rather than the
// samples.
var twiddleWalk = buildTwiddleWalk()

// buildTwiddleWalk samples the curve and quantises it to the field.
func buildTwiddleWalk() []int {
	var walk []int
	previous := -1

	for i := range twiddleSamples {
		t := 2 * math.Pi * float64(i) / float64(twiddleSamples)

		// The lemniscate of Gerono, x = cos t and y = sin t cos t, with y
		// doubled so that both axes span the same range and the curve uses the
		// whole field. Doubling it makes it sin 2t, which is the same curve
		// described a second way.
		x := math.Cos(t)
		y := 2 * math.Sin(t) * math.Cos(t)

		// Both axes are mapped from minus one to plus one onto the field, and
		// rounded rather than truncated, since a truncated curve leans toward the
		// top left corner of the grid it is drawn on.
		col := int(math.Round((x + 1) / 2 * float64(twiddleCols-1)))
		row := int(math.Round((1 - y) / 2 * float64(twiddleRows-1)))

		dot := row*twiddleCols + col
		if dot == previous {
			continue
		}
		walk = append(walk, dot)
		previous = dot
	}

	if len(walk) == 0 {
		return []int{0}
	}
	return walk
}

// Twiddle renders the marquee at a step in its walk.
//
// It takes the step rather than the time so that the caller decides the clock: the
// session owns when a frame is due, and a function that read the clock itself would be a
// second thing that has to agree with the repaint rate.
//
// The return is two braille cells, since the field is two cells wide. A cell with no
// dot in it is a blank, which is what makes the crossing at the centre of the figure
// legible: the dot passes through one cell and the other is empty on either side of it.
func Twiddle(step int) string {
	if len(twiddleWalk) == 0 {
		return ""
	}

	dot := twiddleWalk[((step%len(twiddleWalk))+len(twiddleWalk))%len(twiddleWalk)]
	row, col := dot/twiddleCols, dot%twiddleCols

	var b strings.Builder
	for cell := range twiddleCells {
		b.WriteRune(rune(0x2800 + twiddleCellBits(cell, row, col)))
	}
	return b.String()
}

// brailleBits is where each dot of a cell sits in its bit pattern.
//
// The layout is not row-major: bit 0 is the top-left dot, bit 1 the middle-left, bit
// 2 the bottom-left, bit 3 the top-right, and so on down each column. The bottom row
// is the exception, since the eighth dot is not below the fourth but is the seventh
// bit, so a table is what records the layout and what an arithmetic expression would
// get wrong on the last row.
var brailleBits = [4][2]int{
	{0, 3}, // top row
	{1, 4}, // second row
	{2, 5}, // third row
	{6, 7}, // bottom row, which is not what the pattern above would suggest
}

// twiddleCellBits returns the pattern for one cell of the field, given where the dot
// is overall.
//
// The cell is a parameter rather than worked out from the dot index, so a caller
// rendering a field knows which cell it is writing and this does not have to guess
// which one a dot belongs to.
func twiddleCellBits(cell, row, col int) int {
	if row < 0 || row > twiddleRows-1 {
		return 0
	}

	within := col - cell*2
	if within < 0 || within > 1 {
		return 0
	}
	return 1 << uint(brailleBits[row][within])
}

// TwiddleHue returns the direct-RGB escape for the twiddle at a step.
//
// The form is `38;2;r;g;b` rather than a 256-colour index. The twiddle is the one thing on
// the frame that needs continuous hue: a rotation through 256 indexed cells is not a
// rotation, it is a walk through a palette, and it bands visibly at every boundary. Two
// columns of output are not worth carrying a whole palette to avoid.
func TwiddleHue(step int) string {
	n := len(twiddleWalk)
	hue := float64((((step % n) + n) % n)) * (360 / float64(n))

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
