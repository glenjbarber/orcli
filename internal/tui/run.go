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
// # What this package owns
//
// The footer, and nothing above it. There is no scroll region: the terminal owns the rows
// above the footer, this package writes rows there and never moves the viewport, and the
// reader's own scrollback is the transcript.
//
// The consequence is that the footer is not pinned. A log row long enough to scroll the
// screen pushes the footer up with everything else, and the next paint writes the footer
// again at the bottom rather than where it was. That is the reader's decision rather than
// a fault to repair here, and it is why a keystroke redraws the prompt row alone: the
// footer is terminal rows, so rewriting all of it on every keystroke advanced the screen
// by the footer's height each time.

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
// field and nothing else.
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

// SetSize adopts a new size.
//
// A paint asks the terminal before it draws, since a rule drawn at the width the terminal
// had two resizes ago is a rule the wrong length.
func (s *Screen) SetSize(size WindowSize) {
	s.mu.Lock()
	s.rows, s.cols = size.Rows, size.Cols
	s.mu.Unlock()
}

// Writer returns what this screen writes to, for a caller that has to send a worker's
// output to the terminal rather than into the log.
//
// It is a method rather than the field so a caller holding the Screen cannot reach the
// writer and write to it behind the lock that serialises draws. Two writers interleaving
// produce a row with half a sequence in it.
func (s *Screen) Writer() io.Writer { return s.out }

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
		group:   newGroup(),
	}

	// The window watch runs for the life of the loop and not for the life of the
	// program, since a signal arriving after the loop has gone is a write to a
	// frame nobody is reading.
	defer watchWindow()()

	// Leaving waits for whatever was writing to the frame before the terminal is
	// handed back, and it has to come after the raw mode restore is deferred and
	// before the paste mode is given up, so a turn that is still drawing gets to
	// finish while the terminal is still its own.
	defer l.group.Close()

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

	// group counts the goroutines writing to the frame, and is what leaving waits
	// for.
	group *group

	// editor is the field on the prompt row.
	editor Editor

	// mu guards cancel, which the turn's own goroutine clears while the loop reads
	// it on every paint.
	mu     sync.Mutex
	cancel context.CancelFunc

	// drawn is how many log rows have been painted, so a paint writes the rows that
	// arrived since the last one and never the whole log again.
	drawn int

	// footer is the Bar the last paint drew, and the reason a paint that has nothing
	// new to say writes nothing. It is compared rather than tracked by version, since
	// what the reader sees is the text and not a counter.
	footer Bar

	// footerDrawn reports whether the footer has been painted at all, so the first
	// paint draws it and a later paint that happens to compute the same Bar still
	// knows it was drawn.
	footerDrawn bool

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
// The rows are append-only and are written forward from what was drawn, never redrawn
// whole: the frame's one rule is that the reader's own scrollback is the transcript, and
// a log rewritten under them is a log they cannot scroll back through.
//
// Three cases, decided by what changed:
//
//   - nothing: the footer is left alone and only the caret is placed.
//   - the prompt row alone: the row is rewritten in place, since that is where the
//     reader's typing goes and rewriting the whole footer for one character advances the
//     screen by the footer's height.
//   - anything else: the whole footer is drawn, since a bar or the figure has moved and
//     the rows around it have to move with it.
func (l *interfaceLoop) paint() {
	screen := l.screen

	// The size is adopted before anything is drawn, since a rule drawn at the width the
	// terminal had two resizes ago is a rule the wrong length.
	if size := SizeOf(l.keys.fd); size.Rows >= minSize && size.Cols >= minSize {
		screen.SetSize(size)
	}
	resized()

	palette := paletteOf(l.session)
	rows := l.session.Log().Rows()

	if l.drawn < len(rows) {
		DrawLog(screen, rows[l.drawn:], palette)
		l.drawn = len(rows)
	}

	opts := l.session.Options()
	state, detail := l.session.State()

	footer := Bar{
		Top: RenderTop(string(state)+detailSuffix(detail), "", "", "", "", "", ""),
		Bottom: RenderBottom(l.sessionName(), opts.Mouse, false,
			opts.Provider, opts.Model, "", string(opts.Approval)),
		Field: l.fieldRow(),
	}

	// The figure takes the prompt row while a turn is running, so a step is a change
	// to the footer rather than a separate write above it. It is only carried while a
	// turn is running: a figure over an idle frame is a figure the reader has to learn
	// to ignore, and the prompt row carries the reader's own line then.
	running := false
	switch state {
	case StateThinking, StateWorking:
		l.step++
		running = true
		footer.Twiddle = strings.Repeat(" ", TwiddleIndent) +
			TwiddleHue(l.step) + Twiddle(l.step) + "  " + twiddleWord(state)
	default:
		l.step = 0
	}

	switch {
	case !l.footerDrawn:
		DrawStack(screen, footer, palette)
		l.footer = footer
		l.footerDrawn = true

	case sameFooterExceptField(l.footer, footer):
		// A keystroke, or an edit to the field. Only the prompt row changed, so only
		// that row is written.
		l.drawPromptRow(footer, palette)
		l.footer = footer

	case footer != l.footer:
		DrawStack(screen, footer, palette)
		l.footer = footer
	}

	// The cursor goes back to the prompt row on every paint rather than once at entry,
	// so a reader typing into a caret they cannot see is never the state they are in.
	l.placeCaret(running)
}

// sameFooterExceptField reports whether two footers differ only in the reader's own text.
//
// It is the test that keeps a keystroke from rewriting the footer. The bars and the figure
// are what occupy the rows around the prompt row, and a change to any of them has to move
// them; a change to the field does not, since the field is on the prompt row and that row
// is rewritten where it is.
func sameFooterExceptField(old, next Bar) bool {
	return old.Top == next.Top &&
		old.Bottom == next.Bottom &&
		old.Twiddle == next.Twiddle
}

// sessionName is what the bottom bar calls the session being typed into.
//
// It is the conversation until /run opens another, and the numbering starts at one
// so that "Session 1" is the first session rather than a count of sessions before
// it.
func (l *interfaceLoop) sessionName() string { return "Session 1" }

// twiddleWord is the word beside the figure.
//
// It is the state itself rather than a second set of words, since a figure beside a
// word the reader has to learn is two vocabularies where one would do. The figure shows
// which of the two running states this is, which is what a reader watching a pause
// between the first token and the rest is looking for.
func twiddleWord(state State) string {
	if state == StateThinking {
		return "thinking"
	}
	return "working"
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
		// A report nothing acts on. Mouse reporting is off and /mouse is not wired,
		// so this arrives only from a terminal that was asked for it by something
		// else, and consuming it is what keeps its bytes out of the field.

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
// typing, the figure moves, and escape reaches the cancel below.
//
// The count is raised before the goroutine starts and lowered after its answer has been
// drawn, so a reader who leaves while a turn is in flight waits for it rather than
// handing the terminal back with a request still writing to it. A turn refused by the
// count never started, which is why it is a refusal rather than a silent skip.
func (l *interfaceLoop) start(ctx context.Context, question string) {
	if err := l.group.Add(); err != nil {
		l.session.Notice(err.Error(), 0, RoleFailure)
		return
	}

	turnCtx, cancel := context.WithCancel(ctx)

	l.mu.Lock()
	l.cancel = cancel
	l.mu.Unlock()

	go func() {
		// The count is lowered on every path out, including a panic, since a turn
		// that took the process down does not need the count but a reader who came
		// back would have waited for it for ever.
		defer l.group.Done()

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

	palette := paletteOf(s)
	DrawLog(screen, s.Log().Rows(), palette)

	opts := s.Options()
	state, detail := s.State()
	DrawStack(screen, Bar{
		Top: RenderTop(string(state)+detailSuffix(detail), "", "", "", "", "", ""),
		Bottom: RenderBottom("Session 1", opts.Mouse, false,
			opts.Provider, opts.Model, "", string(opts.Approval)),
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
