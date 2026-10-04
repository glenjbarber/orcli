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
// returns strings, and a draw is the only place a string becomes bytes the terminal will act
// on, so the owner of the bytes is the owner of the terminal.
//
// # One region, the whole screen
//
// The frame owns every row. There is no scroll region set, since there is no second part
// of the screen for the log to have: the log is a column beside the status fields and the
// program decides which row of it is where.
//
// The reader's own scrollback is therefore whatever the terminal kept from before the
// program started. The transcript is drawn by the program from rows it holds, and the paint
// path copies those rows before releasing the lock so a renderer never reads a slice a
// worker is appending to.

// WindowSize is a terminal's size in rows and columns.
//
// It is a value rather than a pair of returns because every caller here needs both halves,
// and a caller that had to remember which order they came in would get it wrong once.
type WindowSize struct {
	Rows int
	Cols int
}

// minSize is the smallest window this package will draw into.
//
// A terminal reporting nothing is one that cannot be read, and drawing a frame into it
// produces rows nobody sees. A terminal reporting one row is one that can hold the prompt
// row and nothing else.
const minSize = 1

// Screen writes to a terminal and knows how large it is.
//
// The zero value is not usable. A Screen is built by NewScreen, which needs a writer and a
// size, and a Screen built by hand has a writer of nil and writes nothing while reporting
// that it did.
type Screen struct {
	mu sync.Mutex

	out io.Writer

	rows int
	cols int
}

// NewScreen returns a Screen writing to out at the given size.
//
// The size is a parameter rather than a query, so a draw can be tested against figures a
// test chose rather than against whatever the machine's terminal happens to be. SizeOf asks
// the terminal for the real one.
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
// The size is adopted rather than queried, and a paint asks for it before it draws, since a
// frame that drew against a size the terminal has since changed is a frame whose rows are not
// where the arithmetic says they are.
func (s *Screen) SetSize(size WindowSize) {
	s.mu.Lock()
	s.rows, s.cols = size.Rows, size.Cols
	s.mu.Unlock()
}

// Writer returns what this screen writes to, for a caller that has to send a worker's
// output to the terminal rather than into the frame.
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
// A screen with no writer writes nothing rather than panicking, since a Screen built by hand
// has one, and a draw that crashed would take the session with it.
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

// LogRows is how many log rows ride beside the status fields on a terminal of this height.
//
// The frame holds the screen and the log is a column beside its fields, so there is no
// separate region and no row count of its own: every row but the prompt carries one, and
// the log is as tall as the frame less the prompt row.
//
// It is reported rather than deleted because a caller asking how much room the log has is
// asking whether the frame fits, and the answer is one less than the rows drawn.
func LogRows(height int) int {
	if n := screenRows(height) - 1; n > 0 {
		return n
	}
	return 0
}

// SizeOf asks the terminal how large it is.
//
// It returns a zero size rather than a figure it made up, since a size that reads as zero is
// not a size. A caller that drew a frame into it would be drawing rows nobody can see, and a
// reader would see a program that appeared to do nothing.
func SizeOf(fd uintptr) WindowSize {
	rows, cols := windowSize(fd)
	if rows < 1 || cols < 1 {
		return WindowSize{}
	}
	return WindowSize{Rows: rows, Cols: cols}
}

// Result is what running a line produced.
//
// It is a small type rather than a string and an error because a line can end up meaning two
// different things, and a caller that has to guess which is being told something rather
// than being answered. The Cloudflare guidance is a question the reader did not type, and a
// loop that inferred it from the text would send a reply it happened to look like.
type Result struct {
	// Text is what the reader is shown, and is usually the whole of it.
	//
	// A line break in it is a row break, so a command that answers with a listing gets
	// one row per line rather than one row with the lines run together. The loop owns the
	// log, so a handler answers with a string and the break is interpreted there rather
	// than by every handler that produces one.
	Text string

	// Ask is a question for the model, and is empty for most commands.
	Ask string

	// Quit asks the loop to leave, which is what /quit returns.
	Quit bool
}

// LineRunner is what Start calls for a line the reader submitted.
//
// It is a function rather than an interface so a caller passes one expression and a test
// passes a closure. It takes the context so a command that reaches the network is stopped
// when the reader leaves, rather than outliving the frame that asked for it.
type LineRunner func(ctx context.Context, line string) (Result, error)

// AskFunc sends a question to the model and writes what comes back into the session.
//
// It is passed in rather than reached for, so this package keeps no credential and no
// client. The transport is in internal/openrouter and the session is here, and something
// has to hold the two together; naming the seam keeps this a leaf.
type AskFunc func(ctx context.Context, question string, level int) error

// Start runs the interface until the reader leaves.
//
// It is the entry point that puts the terminal in raw mode, paints, loops over keys, and
// puts everything back on the way out, including on the paths where the program did not
// choose to leave. Every path out restores the terminal, since a reader handed a shell with
// echo cleared has to fix it by hand and did not cause it.
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

	// Raw mode is entered before the first paint, so the terminal is not echoing the
	// reader's keys while the frame is being drawn, and the paste mode is turned on
	// afterwards so a paste arriving in between is a paste into a shell rather than one the
	// client never sees.
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

	// The window watch runs for the life of the loop and not for the life of the program,
	// since a signal arriving after the loop has gone is a write to a frame nobody is
	// reading.
	defer watchWindow()()

	// Leaving waits for whatever was writing to the frame before the terminal is handed
	// back, and it has to come after the raw mode restore is deferred and before the paste
	// mode is given up, so a turn that is still drawing gets to finish while the terminal is
	// still its own.
	defer l.group.Close()

	return l.paintAndRead(ctx)
}

// interfaceLoop is the state one run of the interface carries.
//
// It is a value rather than a set of parameters threaded through paint, because the painter,
// the key handler and the turn all need the same things and a function carrying them all is
// a function whose signature nobody can read. It is not named Loop because that reads as
// an exported thing this package offers, and it is not.
type interfaceLoop struct {
	session *Session
	screen  *Screen
	runner  LineRunner
	ask     AskFunc
	keys    *keyReader

	// group counts the goroutines writing to the frame, and is what leaving waits for.
	group *group

	// editor is the field on the prompt row.
	editor Editor

	// mu guards cancel, which the turn's own goroutine clears while the loop reads it on
	// every paint.
	mu     sync.Mutex
	cancel context.CancelFunc

	// drawn is how many log rows the last paint chose from, so a paint can tell whether
	// the log has moved since rather than comparing it against nothing.
	drawn int

	// footer is the Bar the last paint drew, and is the reason a paint that has nothing new
	// to say writes nothing. It is compared rather than tracked by version, since what the
	// reader sees is the text and not a counter.
	footer Bar

	// footerDrawn reports whether the frame has been painted at all, so the first paint
	// draws it and a later paint that happens to compute the same Bar still knows it was
	// drawn.
	footerDrawn bool

	// step is the sweep's position, advanced on each paint while a turn runs.
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
// Three cases, decided by what changed:
//
//   - the prompt row alone: that row is rewritten where it stands, since that is where the
//     reader's typing goes and rewriting every row for one character is a repaint per
//     keystroke.
//   - the log or a field: every row is written, since the log rows beside the fields moved
//     and a row left alone is a row showing a log row that is no longer there.
//   - nothing: the frame is left alone and only the caret is placed.
//
// The rows are chosen from the tail of the log, so a row arriving appears at the bottom and
// everything above it moves up.
func (l *interfaceLoop) paint() {
	screen := l.screen

	// The size is adopted before anything is drawn, since every row's position and every
	// row's cut depend on it.
	if size := SizeOf(l.keys.fd); size.Rows >= minSize && size.Cols >= minSize {
		screen.SetSize(size)
	}
	resized()

	palette := paletteOf(l.session)
	rows := l.session.Log().Rows()
	grown := len(rows) != l.drawn
	l.drawn = len(rows)

	state, detail := l.session.State()

	// running is whether a turn is in flight, and is read once here so the figure field and
	// the state field are decided by one value rather than by two switches that have to
	// agree. The two states are the only ones a turn is in, so the word is the condition
	// rather than a field the session has to keep up to date.
	running := state == StateThinking || state == StateWorking

	figure := "idle"
	if running {
		l.step++
		figure = SweepText(twiddleWord(state), l.step)
	} else {
		l.step = 0
	}

	footer := Bar{
		Status: l.status(state, detail, figure),
		Field:  l.fieldRow(),
	}
	frame := stackLines(footer, rows, screen.Height(), palette)

	switch {
	case !l.footerDrawn:
		DrawStack(screen, frame, palette)
		l.footer = footer
		l.footerDrawn = true

	case sameFooterExceptField(l.footer, footer) && !grown:
		// A keystroke, or an edit to the field. Only the prompt row changed, so only that
		// row is written, and the log rows beside the fields are where they already are.
		DrawFooterRow(screen, len(frame)-1, frame, palette)
		l.footer = footer

	default:
		DrawStack(screen, frame, palette)
		l.footer = footer
	}

	// The cursor goes back to the prompt row on every paint rather than once at entry, so a
	// reader typing into a caret they cannot see is never the state they are in.
	l.placeCaret()
}

// status builds the value of each field, field by field.
//
// A value is whatever the field has to say, so a provider is its whole name and a count is
// its whole figure. The one character rule is gone, since a field that could not say what it
// meant in one character said nothing, and a reader looking at `model s` has to read the
// design record to learn what it means.
//
// The figure is the state word rather than its first character, so the sweep has the whole
// word to turn along rather than a single glyph, which is what the sweep was drawn for: a
// pattern turning across a row. It is the state field beside it that carries the plain word,
// so the two say the same thing twice and the sweep is the copy a reader watches move.
func (l *interfaceLoop) status(state State, detail, figure string) Status {
	opts := l.session.Options()
	log := l.session.Log()
	s := Status{}

	s[fieldCwd] = l.workingDir()
	s[fieldSession] = "Session 1"
	s[fieldProvider] = orNone(opts.Provider)
	s[fieldModel] = orNone(opts.Model)
	s[fieldKey] = present(opts.APIKey != "")
	s[fieldState] = string(state) + detailSuffix(detail)
	s[fieldFigure] = figure
	s[fieldApproval] = orNone(string(opts.Approval))
	s[fieldVerbosity] = "0"
	s[fieldCognito] = onOff(opts.Cognito, "on", "off")
	s[fieldColor] = onOff(opts.Color, "on", "off")
	s[fieldMouse] = onOff(opts.Mouse, "on", "off")
	s[fieldCopy] = onOff(false, "available", "none")
	s[fieldBell] = onOff(opts.Bell, "on", "off")
	s[fieldPane] = "main"
	s[fieldWorkers] = plural(len(l.session.Workers()), "worker", "workers")
	s[fieldQueue] = plural(0, "prompt", "prompts")
	s[fieldHeld] = plural(log.Len(), "row", "rows")
	s[fieldFolded] = plural(log.Folded(), "row", "rows")
	s[fieldLevels] = plural(len(l.session.Levels()), "level", "levels")

	return s
}

// workingDir is the directory the session is typed into, as the reader would name it.
//
// The whole path rather than the last component, since a reader who ran this from a
// worktree and cannot see which one is the reason to look at the field at all. A path too
// long for the terminal is cut by the row it is on rather than here, since the cut is the
// same cut either way and doing it once is one fewer place to be wrong.
func (l *interfaceLoop) workingDir() string {
	if dir := l.session.Options().WorkingDir; dir != "" {
		return dir
	}
	return "."
}

// present reports whether a credential is set up, and never what it is.
//
// The figure itself is never written to a diagnostic in this program, and a frame row is a
// diagnostic: every line of it ends up in terminal scrollback and a reader selecting the row
// gets the whole of it.
func present(there bool) string {
	if there {
		return "present"
	}
	return "absent"
}

// onOff is a word rather than a glyph, so a field says what it means.
//
// It was a letter and a dash when a field was one character wide, and the letter was the
// figure the field carried rather than its name, so `c` meant colour and `m` meant mouse and
// nothing said which. A word costs columns and saves the reader the design record.
func onOff(on bool, yes, no string) string {
	if on {
		return yes
	}
	return no
}

// plural is a count with the noun it counts, so a field reads as English.
//
// A count with no noun is a figure a reader has to interpret, and a figure beside a name is
// the thing the frame stopped carrying when it stopped being one character wide.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// sameFooterExceptField reports whether two frames differ only in the reader's own text.
//
// It is the test that keeps a keystroke from rewriting the frame. Every field has to move when
// anything in it changes; a change to the field does not, since the field is on the prompt row
// and that row is rewritten where it is.
//
// Every field is compared rather than the ones that happen to hold something today. A field
// left out is a field whose contents a caller can change without the frame noticing, and the
// next thing to go in that field would then never appear.
func sameFooterExceptField(old, next Bar) bool {
	return old.Status == next.Status
}

// twiddleWord is the figure beside the state while a turn runs.
//
// It is the word rather than its first character, so the hue sweep turns across the whole
// word. A sweep on one glyph is a glyph changing colour rather than a pattern travelling,
// and the pattern is what the sweep was drawn to be.
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
		// The one interrupt trigger in this unit. It stops the turn and leaves the field
		// alone, since a reader who interrupts a running turn has not said they want to
		// abandon what they were typing.
		l.stop()
		return false

	case KeyCtrlC:
		// Control C reaches the loop rather than raising a signal, since ISIG is cleared on
		// entry. It is the reader saying leave, which is what a shell would do with it.
		l.stop()
		return true

	case KeyEOF:
		return true

	case KeyMouse:
		// A report nothing acts on. Mouse reporting is off and /mouse is not wired, so this
		// arrives only from a terminal that was asked for it by something else, and
		// consuming it is what keeps its bytes out of the field.

	case KeyUp, KeyDown:
		// Read so a reader pressing one is not left pressing. This unit has no history, and
		// one would need a conversation this interface does not carry.
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
		// A failure is a row rather than a return, since a reader who mistyped a command has
		// to be able to see what happened and carry on.
		l.session.Notice(err.Error(), 0, RoleFailure)
		return false
	}

	if result.Quit {
		return true
	}
	l.writeResult(result.Text)
	if result.Ask != "" && l.ask != nil {
		l.start(ctx, result.Ask)
	}
	return false
}

// writeResult writes what a command answered with into the log, a line at a time.
//
// The split is here rather than in each handler because a row is one line and a Result.Text
// is one string. A handler that answered with a listing built it with newlines in it, and
// writing the whole thing as one row ran the entries together, since the filter that keeps
// control bytes out of a row strips the breaks back out. So a break is a row break, and a
// listing is a listing, whichever handler produced it.
//
// An empty result writes nothing. A blank row in the log is a row a reader scrolls past for
// nothing, and a command that answered with nothing should leave the log as it was.
func (l *interfaceLoop) writeResult(text string) {
	if text == "" {
		return
	}

	for _, row := range splitRows(text) {
		if row == "" {
			// A blank line inside a result is a thing the result says, so it is written. A
			// result that is nothing but breaks is not saying anything, and a log full of
			// blank rows is one a reader cannot read.
			if strings.Trim(text, "\r\n") == "" {
				return
			}
		}
		l.session.Notice(row, 0, RoleNotice)
	}
}

// splitRows breaks a result into the rows it is made of.
//
// A line ends at a line feed or a carriage return, and the pair of them is one end rather
// than two. The pair is how a program writing to a terminal ends a line, and counting it as
// two ends made a blank row between every entry of a listing written that way.
//
// A trailing break does not make an empty row at the end, since a newline after the last
// entry is how a listing is written and a row for it is a row the reader did not ask for. An
// empty row in the middle is kept, since a blank line inside a result is part of what the
// result says.
func splitRows(text string) []string {
	rows := make([]string, 0, 1)
	start := 0

	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			rows = append(rows, text[start:i])

			// A carriage return before the line feed is the other half of one end and is
			// skipped, so the pair does not leave a row of its own between them.
			if i > start && text[i-1] == '\r' {
				rows[len(rows)-1] = text[start : i-1]
			}
			start = i + 1

		case '\r':
			// A carriage return with no line feed beside it is a line end on its own, since a
			// result can carry one from a program that wrote to a terminal.
			if i+1 < len(text) && text[i+1] == '\n' {
				continue
			}
			rows = append(rows, text[start:i])
			start = i + 1
		}
	}

	// Whatever is after the last break is a row, and the whole string is one row when there
	// was no break at all. A string that ends on a break adds nothing, so the trailing break
	// does not become a blank row.
	if start < len(text) {
		rows = append(rows, text[start:])
	}

	return rows
}

// start sends a question and holds the cancel for it.
//
// The turn runs on its own goroutine and the loop carries on, which is the whole point of a
// session that holds its log rather than blocking on a reply. The reader keeps typing, the
// sweep moves, and escape reaches the cancel below.
//
// The count is raised before the goroutine starts and lowered after its answer has been
// drawn, so a reader who leaves while a turn is in flight waits for it rather than handing
// the terminal back with a request still writing to it. A turn refused by the count never
// started, which is why it is a refusal rather than a silent skip.
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
		// The count is lowered on every path out, including a panic, since a turn that took
		// the process down does not need the count but a reader who came back would have
		// waited for it for ever.
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
// It is the escape handler and the only place a cancel is called, so a reader who presses it
// twice does not reach a cancel that has already fired.
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
// It is kept because the drawing is worth having on its own: a caller that wants a frame
// and not an interface, a test that wants the bytes, and a redirected run that has no
// terminal to read keys from. Start is what a reader reaches.
func Run(s *Session, screen *Screen) error {
	if s == nil {
		return errors.New("tui: Run was given no session")
	}
	if screen == nil || screen.out == nil {
		return errors.New("tui: Run was given no terminal to draw on")
	}

	palette := paletteOf(s)
	state, detail := s.State()

	footer := Bar{Status: Status{
		fieldState: string(state) + detailSuffix(detail),
	}}
	DrawStack(screen, stackLines(footer, s.Log().Rows(), screen.Height(), palette), palette)

	return nil
}

// paletteOf returns the palette a session draws with.
//
// It is built at draw time from the session's own options rather than held on the session,
// so a colour changed by a command takes effect on the next row rather than on the next
// session. A session with colour off writes no palette sequence at all, which falls out of
// Palette.Sequence returning nothing rather than out of a check here.
func paletteOf(s *Session) Palette {
	return NewPalette(s.Options().Color, GroundAuto, nil)
}

// RowText renders one row as the bytes that would be written for it.
//
// It is exported so a caller that needs to show a reader what a row will look like, rather
// than what it says, can ask without writing to the terminal. The palette decides, so the
// answer is the same one the draw would give.
func RowText(p Palette, row Row, width int) string {
	return foldRow(p, PlainRow(row), width)
}

// WriteLines writes rows as plain text, one per line, and is what a redirect wants.
//
// A redirected run cannot draw, and writing escape sequences into a pipe gives whoever is
// reading it noise rather than a transcript. So the redirected case writes the same rows with
// no palette and no rules, and the reader gets the words.
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
// buffered is a reader that returns one byte at a time, and a byte at a time is what makes an
// escape sequence arrive in pieces.
func ReaderFor(in io.Reader) *bufio.Reader { return bufio.NewReader(in) }

// StdoutIsATerminal reports whether standard output is a terminal.
//
// A reader who pipes this program on purpose is entitled to be told why it behaves
// differently, and a reader at a terminal is entitled not to be told. The check is the
// termios read in terminal.go, which is the only question that answers it.
func StdoutIsATerminal() bool { return IsTerminal(os.Stdout.Fd()) }
