package tui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Screen owns the terminal the interface draws on.
//
// It is the one thing in this package that writes to a file descriptor. Everything else
// returns strings, and a draw is the only place a string becomes bytes the terminal will
// act on, so the owner of the bytes is the owner of the terminal.
//
// # The scroll region
//
// The log and the footer stack share one screen, so the terminal has to be told where the
// log ends. That is DECSTBM, set on entry and recomputed on every resize, with the log
// clipped to rows `1..H-stackRows`. Without it the stack would scroll away the first
// time a row was written, and the reader would lose the prompt they are typing into.
//
// This is the one piece of the frame with no precedent in the tree, and the piece most
// likely to be wrong. It lives here rather than in a screen.go of its own because a
// scroll region that is set by one function and recomputed by another is two things that
// have to agree about the same number.

// WindowSize is a terminal's size in rows and columns.
//
// It is a value rather than a pair of returns because every caller here needs both
// halves, and a caller that had to remember which order they came in would get it wrong
// once.
type WindowSize struct {
	Rows int
	Cols int
}

// minSize is the smallest window this package will draw into.
//
// A terminal reporting nothing is one that cannot be read, and drawing a frame into it
// produces rows nobody sees. A terminal reporting one row is one that can hold the
// prompt and nothing else.
const minSize = 1

// Screen writes to a terminal and knows how large it is.
//
// The zero value is not usable. A Screen is built by NewScreen, which needs a writer and
// a size, and a Screen built by hand has a writer of nil and writes nothing while
// reporting that it did.
type Screen struct {
	mu sync.Mutex

	out io.Writer

	rows int
	cols int
}

// NewScreen returns a Screen writing to out at the given size.
//
// The size is a parameter rather than a query, so a draw can be tested against figures a
// test chose rather than against whatever the machine's terminal happens to be. SizeOf
// asks the terminal for the real one.
func NewScreen(out io.Writer, size WindowSize) *Screen {
	return &Screen{out: out, rows: size.Rows, cols: size.Cols}
}

// Size returns the terminal's size as last read or set.
func (s *Screen) Size() WindowSize {
	s.mu.Lock()
	defer s.mu.Unlock()
	return WindowSize{Rows: s.rows, Cols: s.cols}
}

// Height returns the number of rows.
func (s *Screen) Height() int { return s.Size().Rows }

// Width returns the number of columns.
func (s *Screen) Width() int { return s.Size().Cols }

// SetSize adopts a new size and recomputes the scroll region.
//
// The region is recomputed rather than left, since a terminal resized taller gives the
// stack rows it did not have and one resized shorter takes them away. A region left at the
// old size clips the log to a height the screen no longer has, which is a log that stops
// growing with no way to see why.
func (s *Screen) SetSize(size WindowSize) {
	s.mu.Lock()
	s.rows, s.cols = size.Rows, size.Cols
	s.mu.Unlock()

	s.SetScrollRegion(logRows(size.Rows))
}

// Write sends text to the terminal.
//
// It is the only method that writes, so the lock that serialises draws lives here rather
// than in each caller. Two writers interleaving produce a row with half a sequence in it,
// which is a colour that bleeds into the next row.
//
// A screen with no writer writes nothing rather than panicking, since a Screen built by
// hand has one, and a draw that crashed would take the session with it.
func (s *Screen) Write(text string) {
	if text == "" || s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.out == nil {
		return
	}
	io.WriteString(s.out, text)
}

// SetScrollRegion tells the terminal where the log ends.
//
// DECSTBM takes the first and last row of the region, both counted from one. The region
// ends one row above the stack, so the log cannot scroll the prompt off the screen. A
// size too small for a region is left alone rather than set to something that hides the
// log, since a wrong region is worse than none.
func (s *Screen) SetScrollRegion(logHeight int) {
	rows, cols := s.Size().Rows, s.Size().Cols
	if rows < minSize || cols < minSize {
		return
	}
	if logHeight < 1 {
		logHeight = 1
	}
	if logHeight > rows {
		logHeight = rows
	}

	s.Write(fmt.Sprintf("\x1b[%d;%dr", 1, logHeight))
}

// SizeOf asks the terminal how large it is.
//
// It returns a zero size rather than a figure it made up, since a size that reads as zero
// is not a size. A caller that drew a frame into a zero would be drawing rows nobody can
// see, and a reader would see a program that appeared to do nothing.
func SizeOf(fd uintptr) WindowSize {
	rows, cols := windowSize(fd)
	if rows < 1 || cols < 1 {
		return WindowSize{}
	}
	return WindowSize{Rows: rows, Cols: cols}
}

// Run draws the session until it ends.
//
// This is the first thing in the package that paints, and the shape of it is decided
// rather than open: the log grows downward, the stack is held at the bottom, and nothing
// is redrawn except the twiddle. A frame that redrew itself whole would be the model
// this package was rebuilt to leave behind.
//
// The session is read, not driven. Nothing here reads keys and nothing here sends a
// request, because a line editor and a turn loop are separate units with their own
// decisions. What this owns is the terminal: the scroll region, the rows, and the width
// every row is cut to.
//
// A nil session or a screen with no writer is reported rather than drawn into, since a
// silent draw writes nothing and a reader concludes the program is idle.
func Run(s *Session, screen *Screen) error {
	if s == nil {
		return errors.New("tui: Run was given no session")
	}
	if screen == nil || screen.out == nil {
		return errors.New("tui: Run was given no terminal to draw on")
	}

	// The region is set once here rather than per draw, and recomputed on every
	// resize, since a region set per draw would be a region the reader can see being
	// rebuilt under them.
	screen.SetScrollRegion(logRows(screen.Height()))

	palette := paletteOf(s)
	DrawLog(screen, s.Log().Rows(), palette)

	state, detail := s.State()
	DrawStack(screen, Bar{
		Provider: RenderProvider(s.Options().Provider, s.Options().Model, state, detail,
			string(s.Options().Approval)),
	}, palette)

	return nil
}

// paletteOf returns the palette a session draws with.
//
// It is built at draw time from the session's own options rather than held on the
// session, so a colour changed by a command takes effect on the next row rather than on
// the next session. A session with colour off writes no palette sequence at all, which
// falls out of Palette.Sequence returning nothing rather than out of a check here.
func paletteOf(s *Session) Palette {
	return NewPalette(s.Options().Color, GroundAuto, nil)
}

// RowText renders one row as the bytes that would be written for it.
//
// It is exported so a caller that needs to show a reader what a row will look like,
// rather than what it says, can ask without writing to the terminal. The palette
// decides, so the answer is the same one the draw would give.
func RowText(p Palette, row Row, width int) string {
	return foldRow(p, PlainRow(row), width)
}

// WriteLines writes rows as plain text, one per line, and is what a redirect wants.
//
// A redirected run cannot draw, and writing escape sequences into a pipe gives whoever
// is reading it noise rather than a transcript. So the redirected case writes the same
// rows with no palette and no rules, and the reader gets the words.
func WriteLines(w io.Writer, rows []Row) error {
	var b strings.Builder
	for _, row := range rows {
		plain := PlainRow(row)
		b.WriteString(plain.Text)
		b.WriteByte('\n')
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// ReaderFor returns a buffered reader over in, for a caller that reads keys.
//
// It is here rather than at the call sites because a reader over a terminal that is not
// buffered is a reader that returns one byte at a time, and a byte at a time is what
// makes an escape sequence arrive in pieces.
func ReaderFor(in io.Reader) *bufio.Reader { return bufio.NewReader(in) }

// StdoutIsATerminal reports whether standard output is a terminal.
//
// A reader who pipes this program on purpose is entitled to be told why it behaves
// differently, and a reader at a terminal is entitled not to be told. The check is the
// termios read in terminal.go, which is the only question that answers it.
func StdoutIsATerminal() bool { return IsTerminal(os.Stdout.Fd()) }
