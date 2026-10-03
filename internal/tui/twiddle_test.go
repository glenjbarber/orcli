package tui

import (
	"fmt"
	"strings"
	"testing"
)

// TestTwiddleHasTwoCells covers the shape of the figure. One cell is a spinner and two
// cells are a pair, so the count is the first thing to hold and everything else is
// measured against it.
func TestTwiddleHasTwoCells(t *testing.T) {
	got := Twiddle(0)
	if want := len([]rune(got)); want != twiddleCells {
		t.Errorf("Twiddle(0) is %q, which is %d cells, want %d", got, want, twiddleCells)
	}
}

// TestTwiddleWalksEveryFrame covers the period of the walk. A figure that repeats a frame
// inside the period reads as a stutter rather than a turn, so each step must give a
// figure no earlier step gave.
func TestTwiddleWalksEveryFrame(t *testing.T) {
	n := len(twiddleFrames)
	seen := map[string]int{}

	for step := range n {
		frame := Twiddle(step)
		if first, repeated := seen[frame]; repeated {
			t.Errorf("step %d gave %q, which step %d already gave: the walk repeats",
				step, frame, first)
		}
		seen[frame] = step
	}
}

// TestTwiddleCellsAreHalfAPeriodApart covers the reason there are two cells. The right one
// is where the left one was half a period ago, which is what makes a pair read as one
// figure turning rather than as two figures blinking in turn.
func TestTwiddleCellsAreHalfAPeriodApart(t *testing.T) {
	n := len(twiddleFrames)

	for step := range n {
		here, halfOn := []rune(Twiddle(step)), []rune(Twiddle(step+n/2))

		if len(here) != twiddleCells || len(halfOn) != twiddleCells {
			t.Fatalf("step %d gave %q and step %d gave %q, want %d cells each",
				step, Twiddle(step), step+n/2, Twiddle(step+n/2), twiddleCells)
		}

		// The right cell now is the left cell half a period ago, and the left
		// cell now is what the right one becomes half a period on. Both follow
		// from the pair being offset by half the walk.
		if here[1] != halfOn[0] {
			t.Errorf("step %d has %q in the right cell, want %q",
				step, here[1], halfOn[0])
		}
		if halfOn[1] != here[0] {
			t.Errorf("step %d has %q as the left cell half a period on, want %q",
				step, halfOn[1], here[0])
		}
	}
}

// TestTwiddleCompletesItsCycle covers the period. A reader watching for a minute should
// see the same figure come round, and a walk whose period does not divide the hue cycle
// shows a different colour on the same frame each turn.
func TestTwiddleCompletesItsCycle(t *testing.T) {
	n := len(twiddleFrames)

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
// be set before the session started, and a negative step reaching an index below zero is a
// panic rather than a figure.
func TestTwiddleTakesANegativeStep(t *testing.T) {
	last := len(twiddleFrames) - 1

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
	n := len(twiddleFrames)
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
	for step := range len(twiddleFrames) * 4 {
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

// channels reads the three numbers out of a direct-RGB sequence.
func channels(seq string) (r, g, b int, ok bool) {
	_, err := fmt.Sscanf(seq, "\x1b[38;2;%d;%d;%d", &r, &g, &b)
	return r, g, b, err == nil
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
