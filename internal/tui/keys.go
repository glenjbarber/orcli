package tui

import (
	"context"
	"time"
)

// One key, as the loop reads it.
//
// A key is a value rather than a rune because most keys are not runes. An arrow,
// an enter and a backspace are three different things that arrive as three
// different sequences, and a reader whose editor took a rune would have to
// reassemble them here anyway. Holding them as one type means the editor asks
// what arrived rather than guessing from a byte.

// Key is what the reader pressed.
type Key int

// The keys this unit acts on.
//
// The set is named rather than spelled as bytes so that an editor compares against
// a name and a terminal's spelling stays in this file. A key a reader presses
// that is not in this list arrives as KeyRune with its rune, which is what makes
// ordinary typing work without a constant for every letter.
const (
	// KeyNone is what a read that found nothing returns, so the loop has one
	// answer for nothing rather than two.
	KeyNone Key = iota
	// KeyRune is an ordinary character, in the rune itself.
	KeyRune
	// KeyEnter submits the line.
	KeyEnter
	// KeyBackspace deletes the rune before the caret.
	KeyBackspace
	// KeyDelete deletes the rune after the caret.
	KeyDelete
	// KeyLeft and KeyRight move the caret.
	KeyLeft
	KeyRight
	// KeyUp and KeyDown move it through a history, which this unit has none of,
	// and are read so that a reader pressing one is not left pressing.
	KeyUp
	KeyDown
	// KeyHome and KeyEnd move it to the ends of the line.
	KeyHome
	KeyEnd
	// KeyTab completes the word before the caret.
	KeyTab
	// KeyEscape is the lone escape, and the one interrupt trigger in this unit.
	//
	// It is a key rather than a signal, and the reason it exists at all is that a
	// reader pressing it is asking for the turn to stop.
	KeyEscape
	// KeyCtrlC abandons a turn, which is what clearing ISIG on entry made
	// possible: the byte reaches this loop instead of raising a signal.
	KeyCtrlC
	// KeyEOF is control D, and it is the reader saying there is no more input.
	KeyEOF
	// KeyMouse is a mouse report, which nothing acts on and which is named so the
	// loop can hold it rather than mistake it for a sequence of keys.
	KeyMouse
)

// keyReader turns bytes from a descriptor into keys.
//
// It holds a prefix across reads, and that is the whole reason it is a value
// rather than a function: a mouse report arrives in one piece, and a read that
// happened to catch its opening escape alone would hand the loop an interrupt for
// a click the reader never asked for. Holding the prefix until it can be decided
// is what stops that.
type keyReader struct {
	fd uintptr

	// held is a prefix that has been seen and not yet decided.
	held []byte

	// since is when the prefix was first seen, so a prefix that is never
	// decided is not held for the life of the session.
	since time.Time
}

// newKeyReader returns a reader over a descriptor.
func newKeyReader(fd uintptr) *keyReader { return &keyReader{fd: fd} }

// readBufSize is how many bytes one read asks for.
//
// Large enough that a paste arrives in few reads and small enough that the buffer
// is not held longer than the bound of one attempt. A terminal delivers a mouse
// report whole in any case, since it is written in one piece.
const readBufSize = 256

// keyIdleBound is how long one attempt waits before reporting nothing.
//
// It is a long wait by the standard of readPollStep, since the loop is holding
// the frame still while the reader reads or writes. A shorter one would spend a
// core turning the terminal over, and a longer one would make the twiddle stall
// between steps.
const keyIdleBound = 40 * time.Millisecond

// keySequenceBound is how long an undecided prefix is held before it is decided by
// whatever is in it.
//
// A mouse report is six bytes in the older form and a handful more in the SGR
// form, so this is longer than any report and short enough that a stray escape at
// the end of a stream is not held for ever.
const keySequenceBound = 40 * time.Millisecond

// Next returns the next key, and whether one arrived.
//
// A false answer is not a failure. It is the terminal with nothing typed on it,
// which is the state a session spends most of its life in, and the loop paints
// again and asks again.
//
// The context is asked before waiting rather than after, so a reader who has asked
// to leave is not made to wait out the bound first.
func (r *keyReader) Next(ctx context.Context) (Key, rune, bool) {
	for {
		if ctx.Err() != nil {
			return KeyNone, 0, false
		}

		// A held prefix is decided before another read, so a report split across
		// two reads is completed rather than started over.
		if len(r.held) > 0 {
			key, r0, rest, settled := decide(r.held)
			if settled {
				r.consume(len(r.held) - len(rest))
				return key, r0, true
			}
			if time.Since(r.since) >= keySequenceBound {
				// The prefix is older than any report could take to arrive, so
				// it is decided by what is in it rather than held for ever. The
				// escape at the front of it is the interrupt it stands for.
				key, r0, rest := force(r.held)
				r.consume(len(r.held) - len(rest))
				return key, r0, true
			}
		}

		var buf [readBufSize]byte
		n, err := readWithin(r.fd, buf[:], keyIdleBound)
		if err != nil {
			// A descriptor that cannot be read is asked again rather than
			// reported. Reporting it as a failure would end a session over a read
			// that a reader can see was fine, since the frame is still drawn.
			continue
		}
		if n == 0 {
			continue
		}

		if len(r.held) == 0 {
			r.since = time.Now()
		}
		r.held = append(r.held, buf[:n]...)
	}
}

// consume drops n bytes from the front of the held prefix.
//
// It is a method rather than an assignment at each call site so that the prefix
// and the time it was first seen cannot drift apart: a prefix emptied here has
// its time reset, which is what makes the next one a new prefix.
func (r *keyReader) consume(n int) {
	if n >= len(r.held) {
		r.held = nil
		r.since = time.Time{}
		return
	}
	r.held = append([]byte(nil), r.held[n:]...)
}

// decide turns a prefix into a key, and says whether it could.
//
// What a prefix means depends on its bytes and not on how long it has been
// waiting. The time is the caller's, and the caller asks force when it has waited
// long enough.
func decide(prefix []byte) (Key, rune, []byte, bool) {
	if len(prefix) == 0 {
		return KeyNone, 0, nil, false
	}

	// A lone escape is a key, and the interrupt. It is decided here rather than
	// held because it is the one byte at the front of a buffer that could be the
	// opening of a report, and holding it would swallow the interrupt it stands
	// for. The bound in Next is what keeps that from firing on the first byte of
	// a report that has not finished arriving.
	if prefix[0] == 0x1b {
		if len(prefix) == 1 {
			return KeyEscape, 0, nil, true
		}
		return decideEscape(prefix)
	}

	// A control byte is named rather than delivered as a rune, since a reader
	// pressing control C means a turn and not the letter.
	switch prefix[0] {
	case 0x03:
		return KeyCtrlC, 0, nil, true
	case 0x04:
		return KeyEOF, 0, nil, true
	case 0x0d, 0x0a:
		return KeyEnter, 0, nil, true
	case 0x7f, 0x08:
		return KeyBackspace, 0, nil, true
	case 0x09:
		return KeyTab, 0, nil, true
	}

	if prefix[0] < 0x20 {
		// A control byte this interface does not act on. It is consumed rather
		// than delivered, since a reader who pressed it did not mean to type a
		// control character into a prompt.
		return KeyNone, 0, prefix[1:], true
	}

	return KeyRune, rune(prefix[0]), prefix[1:], true
}

// force decides a prefix that waited longer than any report could take.
//
// It exists because the alternative is a prefix that is never settled, and a
// session that stops answering when the reader types a sequence this build has
// never heard of. The escape at the front is reported as the interrupt, since
// that is what a reader pressing it meant.
func force(prefix []byte) (Key, rune, []byte) {
	if prefix[0] == 0x1b {
		return KeyEscape, 0, prefix[1:]
	}
	return KeyRune, rune(prefix[0]), prefix[1:]
}

// decideEscape turns a sequence beginning with an escape into a key.
//
// An arrow is three bytes and is taken whole. A mouse report is named, since it
// arrives in one piece and the loop has nothing to do with it. A sequence nothing
// here recognises is consumed rather than held, since holding one would wait out
// the bound on every keypress a terminal sends that this interface ignores.
//
// The up arrow is the case that decides the order of the checks. It begins like a
// mouse report in the older encoding, where a report is ESC [ M followed by
// three bytes, and it is not one. So the arrow is checked before the report, and
// a sequence that is neither is held until the bound rather than guessed at.
func decideEscape(prefix []byte) (Key, rune, []byte, bool) {
	if len(prefix) < 3 {
		return KeyNone, 0, nil, false
	}

	switch prefix[2] {
	case 'A':
		return KeyUp, 0, prefix[3:], true
	case 'B':
		return KeyDown, 0, prefix[3:], true
	case 'C':
		return KeyRight, 0, prefix[3:], true
	case 'D':
		return KeyLeft, 0, prefix[3:], true
	case 'H':
		return KeyHome, 0, prefix[3:], true
	case 'F':
		return KeyEnd, 0, prefix[3:], true
	case 'M', '<':
		return KeyMouse, 0, nil, true
	}

	return KeyNone, 0, nil, true
}
