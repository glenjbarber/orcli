package tui

import (
	"bufio"
	"context"
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
// is not a size. A caller that drew a frame into it would be drawing rows nobody can see,
// and a reader would see a program that appeared to do nothing.
func SizeOf(fd uintptr) WindowSize {
	rows, cols := windowSize(fd)
	if rows < 1 || cols < 1 {
		return WindowSize{}
	}
	return WindowSize{Rows: rows, Cols: cols}
}

// Result is what running a line produced.
//
// It is a small type rather than a string and an error because a line can end up
// meaning two different things, and a caller that has to guess which is being told
// something rather than being answered. The Cloudflare guidance is a question the
// reader did not type, and a loop that inferred it from the text would send a reply it
// happened to look like.
type Result struct {
	// Text is what the reader is shown, and is usually the whole of it.
	Text string

	// Ask is a question for the model, and is empty for most commands.
	Ask string

	// Quit asks the loop to leave, which is what /quit returns.
	Quit bool
}

// LineRunner is what Start calls for a line the reader submitted.
//
// It is a function rather than an interface so a caller passes one expression and a
// test passes a closure. It takes the context so a command that reaches the network is
// stopped when the reader leaves, rather than outliving the frame that asked for it.
type LineRunner func(ctx context.Context, line string) (Result, error)

// AskFunc sends a question to the model and writes what comes back into the session.
//
// It is passed in rather than reached for, so this package keeps no credential and no
// client. The transport is in internal/openrouter and the session is here, and
// something has to hold the two together; naming the seam keeps this a leaf.
type AskFunc func(ctx context.Context, question string, level int) error

// Start runs the interface until the reader leaves.
//
// It is the entry point that puts the terminal in raw mode, paints, loops over keys,
// and puts everything back on the way out, including on the paths where the program did
// not choose to leave. Every path out restores the terminal, since a reader handed a
// shell with echo cleared has to fix it by hand and did not cause it.
func Start(ctx context.Context, s *Session, screen *Screen, run LineRunner, ask AskFunc) error {
	if s == nil {
		return errors.New("tui: Start was given no session")
	}
	if screen == nil || screen.out == nil {
		return errors.New("tui: Start was given no terminal to draw on")
	}
	if run == nil {
		return errors.New("tui: Start was given no way to run a command")
	}

	if size := screen.Size(); size.Rows < minSize || size.Cols < minSize {
		return fmt.Errorf("tui: %w", ErrNoSize)
	}

	// Raw mode is entered before the first paint, so the terminal is not echoing
	// the reader's keys while the frame is being drawn, and the paste mode is
	// turned on afterwards so a paste arriving in between is a paste into a shell
	// rather than one the client never sees.
	restore, err := enterRaw(os.Stdin.Fd())
	if err != nil {
		return err
	}
	defer restore()
	defer escapeBracketedPasteOff(screen)
	escapeBracketedPaste(screen)

	l := &interfaceLoop{
		session: s,
		screen:  screen,
		runner:  run,
		ask:     ask,
		keys:    newKeyReader(os.Stdin.Fd()),
	}
	return l.paintAndRead(ctx)
}

// interfaceLoop is the state one run of the interface carries.
//
// It is a value rather than a set of parameters threaded through paint, because the
// painter, the key handler and the turn all need the same things and a function
// carrying them all is a function whose signature nobody can read. It is not named Loop
// because that reads as an exported thing this package offers, and it is not.
type interfaceLoop struct {
	session *Session
	screen  *Screen
	runner  LineRunner
	ask     AskFunc
	keys    *keyReader

	// editor is the prompt row's field.
	editor Editor

	// mu guards cancel, which the turn's own goroutine clears while the loop reads
	// it on every paint.
	mu     sync.Mutex
	cancel context.CancelFunc

	// drawn is how many log rows have been painted, so a paint writes the rows that
	// arrived since the last one and never the whole log again.
	drawn int

	// step is the twiddle's position, advanced on each paint while a turn runs.
	step int
}

// paintAndRead paints, reads a key, and acts, until the reader leaves.
func (l *interfaceLoop) paintAndRead(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for {
		l.paint()

		if ctx.Err() != nil {
			return nil
		}

		key, r, ok := l.keys.Next(ctx)
		if !ok {
			continue
		}

		quit := l.act(ctx, key, r)
		if quit {
			return nil
		}
	}
}

// paint draws what has changed since the last one.
//
// The log is append-only and is written forward from what was drawn, never redrawn
// whole: the frame's one rule is that the reader's own scrollback is the transcript,
// and a log rewritten under them is a log they cannot scroll back through. The stack
// is redrawn whole, since it is a fixed number of rows at the bottom.
//
// The scroll region is set here rather than once at entry, so a resize is absorbed by
// the next paint rather than needing a signal of its own.
func (l *interfaceLoop) paint() {
	screen := l.screen
	screen.SetScrollRegion(logRows(screen.Height()))

	palette := paletteOf(l.session)
	rows := l.session.Log().Rows()

	// The rows are written with the region in place, so the terminal scrolls the
	// log rather than the stack. A row reaching the bottom of the region pushes the
	// others up, which is the whole of what this frame does.
	if l.drawn < len(rows) {
		DrawLog(screen, rows[l.drawn:], palette)
		l.drawn = len(rows)
	}

	opts := l.session.Options()
	state, detail := l.session.State()

	DrawStack(screen, Bar{
		Top:    RenderTop(string(state)+detailSuffix(detail), "", "", "", "", "", ""),
		Bottom: RenderBottom(opts.Provider, opts.Model, "", string(opts.Approval)),
		Field:  l.promptRow(),
	}, palette)

	// The twiddle is the one in-place redraw, and only while a turn is running: a
	// figure moving over an idle frame is a figure the reader has to learn to ignore.
	// It is blank when idle, so nothing is written to the row at all.
	switch state {
	case StateThinking, StateWorking:
		l.step++
		DrawTwiddle(screen, palette, l.step, twiddleWord(state))
	default:
		l.step = 0
		clearTwiddleRow(screen)
	}

	// The cursor goes back to the prompt row on every paint rather than once at
	// entry. The twiddle moved it a row up, a resize moved it, and a paint that
	// ended anywhere else would leave a caret in the middle of the log.
	l.placeCaret()
}

// twiddleWord is the word beside the figure on the twiddle row.
//
// It is the state itself rather than a second set of words, since a figure beside a
// word the reader has to learn is two vocabularies where one would do. The twiddle
// shows which of the two running states this is, which is what a reader watching a
// pause between the first token and the rest is looking for.
func twiddleWord(state State) string {
	if state == StateThinking {
		return "thinking"
	}
	return "working"
}

// clearTwiddleRow blanks the row above the prompt.
//
// It moves up and erases rather than writing a space, since the row may hold colour
// from the figure and a space written plainly over a coloured row leaves the colour
// showing through.
func clearTwiddleRow(w *Screen) {
	w.Write(escapeMoveUp + escapeEraseLine + "\x1b[0m")
}

// act applies one key and reports whether the loop should leave.
func (l *interfaceLoop) act(ctx context.Context, key Key, r rune) bool {
	switch key {
	case KeyRune:
		l.editor.Insert(r)

	case KeyBackspace:
		l.editor.Backspace()

	case KeyDelete:
		l.editor.Delete()

	case KeyLeft:
		l.editor.Left()

	case KeyRight:
		l.editor.Right()

	case KeyHome:
		l.editor.Home()

	case KeyEnd:
		l.editor.End()

	case KeyTab:
		l.editor.Complete()

	case KeyEnter:
		return l.submit(ctx)

	case KeyEscape:
		// The one interrupt trigger in this unit. It stops the turn and leaves the
		// field alone, since a reader who interrupts a running turn has not said they
		// want to abandon what they were typing.
		l.stop()
		return false

	case KeyCtrlC:
		// Control C reaches the loop rather than raising a signal, since ISIG is
		// cleared on entry. It is the reader saying leave, which is what a shell
		// would do with it.
		l.stop()
		return true

	case KeyEOF:
		return true

	case KeyMouse:
		// A report nothing acts on. Mouse reporting is off, so this arrives only
		// from a terminal that was asked for it by something else, and consuming it
		// is what keeps its bytes out of the field.

	case KeyUp, KeyDown:
		// Read so a reader pressing one is not left pressing. This unit has no
		// history, and one would need a conversation this interface does not carry.
	}

	return false
}

// submit acts on a line the reader finished typing.
func (l *interfaceLoop) submit(ctx context.Context) bool {
	line := strings.TrimSpace(l.editor.Text())
	if line == "" {
		return false
	}
	l.editor.Reset()

	result, err := l.runner(ctx, line)
	if err != nil {
		// A failure is a row rather than a return, since a reader who mistyped a
		// command has to be able to see what happened and carry on.
		l.session.Notice(err.Error(), 0, RoleFailure)
		return false
	}

	if result.Quit {
		return true
	}
	if result.Text != "" {
		l.session.Notice(result.Text, 0, RoleNotice)
	}
	if result.Ask != "" && l.ask != nil {
		l.start(ctx, result.Ask)
	}
	return false
}

// start sends a question and holds the cancel for it.
//
// The turn runs on its own goroutine and the loop carries on, which is the whole point
// of a session that holds its log rather than blocking on a reply. The reader keeps
// typing, the twiddle moves, and escape reaches the cancel below.
func (l *interfaceLoop) start(ctx context.Context, question string) {
	turnCtx, cancel := context.WithCancel(ctx)

	l.mu.Lock()
	l.cancel = cancel
	l.mu.Unlock()

	go func() {
		err := l.ask(turnCtx, question, 0)

		l.mu.Lock()
		l.cancel = nil
		l.mu.Unlock()
		cancel()

		if err != nil {
			l.session.Notice(err.Error(), 0, RoleFailure)
		}
	}()
}

// stop ends the turn in flight, and says so only if there was one.
//
// It is the escape handler and the only place a cancel is called, so a reader who
// presses it twice does not reach a cancel that has already fired.
func (l *interfaceLoop) stop() {
	l.mu.Lock()
	cancel := l.cancel
	l.cancel = nil
	l.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	l.session.Finished("stopped")
}

// Run draws the session once, with no input.
//
// It is kept because the drawing is worth having on its own: a caller that wants a
// frame and not an interface, a test that wants the bytes, and a redirected run that
// has no terminal to read keys from. Start is what a reader reaches.
func Run(s *Session, screen *Screen) error {
	if s == nil {
		return errors.New("tui: Run was given no session")
	}
	if screen == nil || screen.out == nil {
		return errors.New("tui: Run was given no terminal to draw on")
	}

	screen.SetScrollRegion(logRows(screen.Height()))

	palette := paletteOf(s)
	DrawLog(screen, s.Log().Rows(), palette)

	opts := s.Options()
	state, detail := s.State()
	DrawStack(screen, Bar{
		Top:    RenderTop(string(state)+detailSuffix(detail), "", "", "", "", "", ""),
		Bottom: RenderBottom(opts.Provider, opts.Model, "", string(opts.Approval)),
		Field:  Prompt,
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
