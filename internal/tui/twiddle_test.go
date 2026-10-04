package tui

import (
	"fmt"
	"strings"
	"testing"
)

// TestTwiddleHasTwoCells covers the shape of the figure. The field is four dots wide
// and a braille cell is two, so the figure is two cells and the count is the first
// thing to hold: a figure one cell wide cannot draw a figure eight, since the two
// loops would be narrower than the gap between them.
func TestTwiddleHasTwoCells(t *testing.T) {
	got := Twiddle(0)
	if want := len([]rune(got)); want != twiddleCells {
		t.Errorf("Twiddle(0) is %q, which is %d cells, want %d", got, want, twiddleCells)
	}
}

// TestTwiddleIsASingleDotAtATime covers the whole of what the figure now is. The old
// figure was two dots a step apart, which made it a turning shape rather than a
// travelling one; a reader watching this one should see exactly one dot and no other,
// since a figure that is two dots is a figure that has already been drawn.
func TestTwiddleIsASingleDotAtATime(t *testing.T) {
	for step := range len(twiddleWalk) {
		dots := 0
		for _, r := range Twiddle(step) {
			dots += popcount(int(r) - 0x2800)
		}

		if dots != 1 {
			t.Errorf("step %d drew %d dots, want 1: %q", step, dots, Twiddle(step))
		}
	}
}

// TestTwiddleReachesEveryRowOfTheField covers the figure rather than the spinner. A
// dot that stays in the middle of the field is a dot that happens to be hue-cycled,
// and the whole point of the shape is that the reader sees two loops.
//
// This is the test that caught the curve being wrong: the lemniscate of Gerono has a
// y reaching half as far as its x, so mapped onto a square field unadjusted it uses
// only the middle two rows and reads as a wave.
func TestTwiddleReachesEveryRowOfTheField(t *testing.T) {
	rows := map[int]bool{}
	for step := range len(twiddleWalk) {
		rows[twiddleWalk[step]/twiddleCols] = true
	}

	for row := range twiddleRows {
		if !rows[row] {
			t.Errorf("row %d of the field is never visited, so the figure does not span it", row)
		}
	}
}

// TestTwiddleReachesBothSidesOfTheField covers the other axis, for the same reason:
// a curve that is wide and flat is a line and not a figure.
func TestTwiddleReachesBothSidesOfTheField(t *testing.T) {
	cols := map[int]bool{}
	for step := range len(twiddleWalk) {
		cols[twiddleWalk[step]%twiddleCols] = true
	}

	for col := range twiddleCols {
		if !cols[col] {
			t.Errorf("column %d of the field is never visited", col)
		}
	}
}

// TestTwiddleCrossesItself covers the crossing, which is what makes the shape an
// infinity rather than a circle or a wave. A lemniscate passes through its own middle
// twice in a cycle, once going each way, and a figure eight without that is not one.
//
// A four by four field has no single dot at its exact centre, so the test accepts a
// crossing through any of the four dots nearest it. What it rules out is a curve that
// misses the middle of the field entirely, which is a circle or an oval.
func TestTwiddleCrossesItself(t *testing.T) {
	middle := map[int]bool{
		(twiddleRows/2-1)*twiddleCols + twiddleCols/2:     true,
		(twiddleRows/2-1)*twiddleCols + twiddleCols/2 - 1: true,
		(twiddleRows/2)*twiddleCols + twiddleCols/2:       true,
		(twiddleRows/2)*twiddleCols + twiddleCols/2 - 1:   true,
	}

	visits := 0
	for step := range len(twiddleWalk) {
		if middle[twiddleWalk[step]] {
			visits++
		}
	}

	if visits < 2 {
		t.Errorf("the middle of the field was visited %d times in a cycle, want at "+
			"least 2: the figure does not cross itself", visits)
	}
}

// TestTheWalkMovesOnEveryStep covers the sampling. Consecutive samples landing on the
// same dot are dropped when the walk is built, so a step that does not move is a
// figure that appears to stutter rather than travel.
func TestTheWalkMovesOnEveryStep(t *testing.T) {
	n := len(twiddleWalk)

	for step := range n - 1 {
		if twiddleWalk[step] == twiddleWalk[step+1] {
			t.Errorf("steps %d and %d are both at dot %d: the walk stands still",
				step, step+1, twiddleWalk[step])
		}
	}
}

// TestTwiddleReturnsToWhereItStarted covers the closing of the curve, and with it the
// one step that is allowed to repeat. The last step lands on the dot the walk began
// at, which is what closes the figure, and a curve that ended one dot away from where
// it began would jump once per cycle for a reader watching for a minute.
func TestTwiddleReturnsToWhereItStarted(t *testing.T) {
	first := twiddleWalk[0]
	last := twiddleWalk[len(twiddleWalk)-1]

	if last != first {
		t.Errorf("the walk ends at dot %d and began at dot %d: the curve does not close",
			last, first)
	}
}

// TestTwiddleCompletesItsCycle covers the period. A reader watching for a minute should
// see the same figure come round, and a walk whose period does not divide the hue cycle
// shows a different colour on the same frame each turn.
func TestTwiddleCompletesItsCycle(t *testing.T) {
	n := len(twiddleWalk)

	for step := range n {
		next := step + n

		got, want := Twiddle(step), Twiddle(next)
		if got != want {
			t.Errorf("step %d gave %q and step %d gave %q, want the same frame",
				step, got, next, want)
		}

		if got, want := TwiddleHue(step), TwiddleHue(next); got != want {
			t.Errorf("the hue at step %d is %q and at step %d is %q, want the same colour",
				step, got, next, want)
		}
	}
}

// TestTwiddleTakesANegativeStep covers the boundary. A step arrives from a clock that may
// be set before the session started, and a negative step reaching an index below zero is
// a panic rather than a figure.
func TestTwiddleTakesANegativeStep(t *testing.T) {
	last := len(twiddleWalk) - 1

	for step := -3 * last; step <= 0; step++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Twiddle(%d) panicked: %v", step, r)
				}
			}()
			if got := Twiddle(step); got == "" {
				t.Errorf("Twiddle(%d) gave nothing, want a figure", step)
			}
		}()
	}
}

// TestTwiddleHueTurns covers the property the colour exists for. The hue has to come round
// the wheel and not sit in one corner of it, since a sweep that never leaves a sixth of
// the range is a blinking figure with decoration on it.
func TestTwiddleHueTurns(t *testing.T) {
	n := len(twiddleWalk)
	seen := map[string]int{}

	for step := range n {
		hue := TwiddleHue(step)

		if !strings.HasPrefix(hue, "\x1b[38;2;") {
			t.Errorf("TwiddleHue(%d) is %q, want a direct-RGB sequence", step, hue)
		}
		if first, repeated := seen[hue]; repeated {
			t.Errorf("TwiddleHue(%d) is %q, which step %d already gave: the sweep repeats",
				step, hue, first)
		}
		seen[hue] = step
	}

	if len(seen) != n {
		t.Errorf("%d distinct hues over %d steps, want %d: the sweep does not cover the wheel",
			len(seen), n, n)
	}
}

// TestTwiddleHueStaysInRange covers the clamp. A lightness of one puts the sum exactly at
// one, and a rounding error above it writes as 256, which is two digits and shifts every
// row after the twiddle by a column.
func TestTwiddleHueStaysInRange(t *testing.T) {
	for step := range len(twiddleWalk) * 4 {
		hue := TwiddleHue(step)

		r, g, b, ok := channels(hue)
		if !ok {
			t.Fatalf("TwiddleHue(%d) is %q, want three channels", step, hue)
		}

		for name, v := range map[string]int{"red": r, "green": g, "blue": b} {
			if v < 0 || v > 255 {
				t.Errorf("TwiddleHue(%d) has %s %d, want 0 to 255", step, name, v)
			}
		}
	}
}

// TestTheBrailleLayoutIsWhatTheBlockSays covers the bit table against the block it
// decodes. The layout is not row-major and the bottom row is not what the pattern
// above it suggests, so a table that is wrong in one place puts the dot in a place
// the reader can see.
func TestTheBrailleLayoutIsWhatTheBlockSays(t *testing.T) {
	cell := func(bits ...int) rune {
		var v int
		for _, b := range bits {
			v |= 1 << uint(b)
		}
		return rune(0x2800 + v)
	}

	if got, want := cell(brailleBits[0][0]), rune(0x2801); got != want {
		t.Errorf("the top-left dot is %#x, want %#x", got, want)
	}
	if got, want := cell(brailleBits[3][0]), rune(0x2840); got != want {
		t.Errorf("the bottom-left dot is %#x, want %#x", got, want)
	}

	// The whole left column is bits 0, 1, 2 and 6, and the exception is bit 6
	// rather than a continuation of the run, which is what an arithmetic
	// expression for the bit index would get wrong. The right column is the same
	// with each bit one higher, which is the point of the table being two columns
	// rather than a single index.
	left := cell(brailleBits[0][0], brailleBits[1][0], brailleBits[2][0], brailleBits[3][0])
	if want := rune(0x2847); left != want {
		t.Errorf("the whole left column is %#x, want %#x", left, want)
	}
	right := cell(brailleBits[0][1], brailleBits[1][1], brailleBits[2][1], brailleBits[3][1])
	if want := rune(0x28B8); right != want {
		t.Errorf("the whole right column is %#x, want %#x", right, want)
	}

	// All eight is the last cell of the block, which is the bound on the table.
	all := cell(0, 1, 2, 3, 4, 5, 6, 7)
	if want := rune(0x28FF); all != want {
		t.Errorf("eight dots is %#x, want %#x", all, want)
	}
}

// channels reads the three numbers out of a direct-RGB sequence.
func channels(seq string) (r, g, b int, ok bool) {
	_, err := fmt.Sscanf(seq, "\x1b[38;2;%d;%d;%d", &r, &g, &b)
	return r, g, b, err == nil
}

// popcount counts the dots set in a braille cell.
func popcount(bits int) int {
	n := 0
	for bits != 0 {
		n += bits & 1
		bits >>= 1
	}
	return n
}

// TestToByteClamps covers the helper directly, since a clamp is the only thing standing
// between a rounding error and a row that is a column too wide.
func TestToByteClamps(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 128},
		{1, 255},
		{1.5, 255},
	}

	for _, c := range cases {
		if got := toByte(c.in); got != c.want {
			t.Errorf("toByte(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
