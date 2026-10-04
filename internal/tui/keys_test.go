package tui

import (
	"context"
	"testing"
	"time"
)

// TestAnOrdinaryRuneIsAKey covers the common case. A byte that is not a control is a
// character the reader typed, and a reader typing anything at all has to see it appear.
func TestAnOrdinaryRuneIsAKey(t *testing.T) {
	key, r, rest, settled := decide([]byte("a"))

	if !settled {
		t.Fatal("an ordinary byte was not settled")
	}
	if key != KeyRune || r != 'a' {
		t.Errorf("gave %v %q, want a rune carrying a", key, r)
	}
	if len(rest) != 0 {
		t.Errorf("a byte was left over: %q", rest)
	}
}

// TestAKeyIsHeldWhenItIsFollowed covers the property that makes this a block reader
// rather than a byte reader. Every key is one byte followed by whatever is next, so
// deciding on one byte is deciding on a prefix.
func TestAKeyIsHeldWhenItIsFollowed(t *testing.T) {
	key, r, rest, settled := decide([]byte("ab"))
	if !settled {
		t.Fatal("two bytes were not settled, want the first with the second held")
	}
	if key != KeyRune || r != 'a' {
		t.Errorf("gave %v %q, want a rune carrying a", key, r)
	}
	if string(rest) != "b" {
		t.Errorf("the second byte was not kept: %q", rest)
	}
}

// TestALoneEscapeIsTheInterrupt covers the one case where deciding on a prefix would
// swallow something. The escape is the one byte at the front of a buffer that could be
// the opening of a mouse report, and it is the interrupt a reader is asking for.
//
// It settles on one byte by design. Holding it would swallow every interrupt, since a
// reader who pressed it alone would be waiting for bytes that are never coming. The
// protection against a report arriving in pieces is in Next, which reads in blocks and
// holds a prefix there rather than here.
func TestALoneEscapeIsTheInterrupt(t *testing.T) {
	key, _, _, settled := decide([]byte{0x1b})

	if !settled {
		t.Fatal("a lone escape was not settled")
	}
	if key != KeyEscape {
		t.Errorf("a lone escape gave %v, want KeyEscape", key)
	}
}

// TestAnArrowIsTakenWhole covers a sequence. Deciding on its first byte would deliver
// an escape to the reader for every arrow they press.
func TestAnArrowIsTakenWhole(t *testing.T) {
	cases := map[string]Key{
		"\x1b[A": KeyUp,
		"\x1b[B": KeyDown,
		"\x1b[C": KeyRight,
		"\x1b[D": KeyLeft,
		"\x1b[H": KeyHome,
		"\x1b[F": KeyEnd,
	}

	for seq, want := range cases {
		key, _, _, settled := decide([]byte(seq))

		if !settled {
			t.Errorf("%q was not settled", seq)
			continue
		}
		if key != want {
			t.Errorf("%q gave %v, want %v", seq, key, want)
		}
	}
}

// TestASequencePrefixIsHeld covers the timing rather than the shape. An escape on its own
// is a key, and an escape followed by a bracket is not yet either, since it could still
// be the opening of a report.
func TestASequencePrefixIsHeld(t *testing.T) {
	if _, _, _, settled := decide([]byte("\x1b[")); settled {
		t.Error("an escape and a bracket were settled, want them held")
	}
}

// TestAMouseReportIsNotAnInterrupt is the case the whole design turns on. A mouse report
// begins with an escape and a bracket, so a reader who clicks has their click delivered
// as an interrupt unless the sequence is recognised before the escape is taken as a key.
//
// The order of the checks is what does it: the escape is only a key on its own, and the
// bracket behind it is what makes it a prefix to be held.
func TestAMouseReportIsNotAnInterrupt(t *testing.T) {
	for _, report := range []string{
		"\x1b[M !!",
		"\x1b[<0;10;20M",
		"\x1b[<0;10;20m",
	} {
		key, _, _, settled := decide([]byte(report))

		if !settled {
			continue
		}
		if key == KeyEscape {
			t.Errorf("a mouse report gave KeyEscape, want it held as a report: %q", report)
		}
		if key != KeyMouse {
			t.Errorf("a mouse report gave %v, want KeyMouse", key)
		}
	}
}

// TestTheUpArrowIsNotAReport is the case that decides the order of the checks. In the
// older encoding a report begins ESC [ M, and the up arrow begins ESC [ A, so the two
// share a prefix and the arrow is the shorter. Reading the arrow as a report would
// swallow the key a reader presses to reach the history.
func TestTheUpArrowIsNotAReport(t *testing.T) {
	key, _, _, settled := decide([]byte("\x1b[A"))

	if !settled || key != KeyUp {
		t.Errorf("the up arrow gave %v, want KeyUp", key)
	}
}

// TestTheControlKeysAreNamed covers the ones a reader presses that are not characters.
// A control byte delivered as a rune is a reader pressing control C and getting the
// letter C.
func TestTheControlKeysAreNamed(t *testing.T) {
	cases := map[byte]Key{
		0x03: KeyCtrlC,
		0x04: KeyEOF,
		0x0d: KeyEnter,
		0x0a: KeyEnter,
		0x7f: KeyBackspace,
		0x08: KeyBackspace,
		0x09: KeyTab,
	}

	for b, want := range cases {
		key, _, _, settled := decide([]byte{b})

		if !settled {
			t.Errorf("byte %#x was not settled", b)
			continue
		}
		if key != want {
			t.Errorf("byte %#x gave %v, want %v", b, key, want)
		}
	}
}

// TestAControlByteThisClientDoesNotUseIsConsumed covers the remainder. A reader who
// pressed it did not mean to type a control character into a prompt, so it is taken and
// dropped rather than delivered.
func TestAControlByteThisClientDoesNotUseIsConsumed(t *testing.T) {
	key, _, rest, settled := decide([]byte{0x01, 'a'})

	if !settled {
		t.Fatal("a control byte was not settled")
	}
	if key != KeyNone {
		t.Errorf("an unused control byte gave %v, want KeyNone", key)
	}
	if string(rest) != "a" {
		t.Errorf("the byte after it was not kept: %q", rest)
	}
}

// TestNothingIsNothing covers the boundary, so a caller has one answer for an empty
// buffer rather than two.
func TestNothingIsNothing(t *testing.T) {
	if _, _, _, settled := decide(nil); settled {
		t.Error("an empty prefix was settled, want it held")
	}
}

// TestForcedDecidesAReportPrefixAsAnInterrupt covers the bound. A prefix older than any
// report could take to arrive is decided by what is in it rather than held for ever,
// and the escape at the front of it is the interrupt rather than a swallowed key.
func TestForcedDecidesAReportPrefixAsAnInterrupt(t *testing.T) {
	key, _, _ := force([]byte{0x1b})
	if key != KeyEscape {
		t.Errorf("a forced prefix gave %v, want KeyEscape", key)
	}

	key, r, rest := force([]byte("ab"))
	if key != KeyRune || r != 'a' || string(rest) != "b" {
		t.Errorf("forcing %q gave %v %q %q", "ab", key, r, rest)
	}
}

// TestTheIdleBoundIsLongerThanAPollStep covers the rates. A key reader that spun at the
// poll step would spend a core turning the terminal over, and one that waited a second
// would make the twiddle stall between frames.
func TestTheIdleBoundIsLongerThanAPollStep(t *testing.T) {
	if keyIdleBound <= readPollStep {
		t.Errorf("the key reader waits %v while a read step is %v, want it longer",
			keyIdleBound, readPollStep)
	}
}

// TestTheSequenceBoundIsLongerThanAReport covers the other bound. A mouse report is six
// bytes in the older form and a handful more in the SGR form, so a bound shorter than
// the report would decide half of one as an interrupt.
func TestTheSequenceBoundIsLongerThanAReport(t *testing.T) {
	if keySequenceBound <= 8*time.Millisecond {
		t.Errorf("the sequence bound is %v, want it longer than a report arriving in "+
			"one write", keySequenceBound)
	}
}

// TestTheReaderStopsWhenTheContextIsDone covers the exit a reader chose. The context is
// asked before waiting rather than after, so a reader who asked to leave is not made to
// wait out the bound first.
func TestTheReaderStopsWhenTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := newKeyReader(0)
	if _, _, ok := r.Next(ctx); ok {
		t.Error("a key was reported after the context was done")
	}
}

// TestTheReaderDropsAStalePrefix covers the bound in the reader rather than in decide. A
// prefix held for longer than any report could take to arrive is decided by what is in
// it, so a session does not stop answering when a reader types a sequence this build
// has never heard of.
func TestTheReaderDropsAStalePrefix(t *testing.T) {
	r := newKeyReader(0)
	r.held = []byte("something")
	r.since = time.Now().Add(-2 * keySequenceBound)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The read will find nothing on descriptor zero, so this asserts the prefix was
	// decided rather than held, which is what a stale prefix is for.
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Next(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * keyIdleBound):
		t.Error("a stale prefix was held rather than decided")
	}
}
