package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ErrNoTerminal reports a run where tcell could not open the terminal screen.
var ErrNoTerminal = errors.New("stdin and stdout must both be terminals")

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
//
// silent tells the implementation not to write the question itself into the log as a
// row, while still sending it to the model and still delivering whatever comes back
// through the normal reply path. The startup HELO is the one caller that passes
// true: its text is an instruction aimed at the model ("greet the reader, using the
// capability message above"), not something the reader typed, and showing it as a
// row would read as a second voice in the transcript that nobody wrote. A reader's
// own line, and a command's Result.Ask, both pass false - what they type is exactly
// what should appear.
type AskFunc func(ctx context.Context, question string, level int, silent bool) error

// Key is the editing action selected from a tcell event.
type Key int

const (
	KeyNone Key = iota
	KeyRune
	KeyEnter
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyTab
	KeyEscape
	KeyCtrlC
	KeyEOF
	KeyMouse
	KeyScrollUp
	KeyScrollDown
	KeyScrollPageUp
	KeyScrollPageDown
	KeyCtrlA
	KeyCtrlW
	KeyCtrlU
	KeyCtrlT
	KeyCtrlB
)

// Start runs the interface until the reader leaves.
//
// It is the entry point that puts the terminal in raw mode, paints, loops over keys, and
// puts everything back on the way out, including on the paths where the program did not
// choose to leave. Every path out restores the terminal, since a reader handed a shell with
// echo cleared has to fix it by hand and did not cause it.
//
// helo, when not empty, is sent through ask once, automatically, before the reader has
// pressed a key - the same path a typed question takes, run on the same goroutine
// mechanics as any other turn. It is a caller's choice, not this package's: a caller with
// no model configured, or one asking in a mode where an unprompted turn would be unwelcome,
// passes an empty string and nothing is sent. Start does not decide whether to greet, only
// how, once asked to.
func startScreenMode(ctx context.Context, s *Session, run LineRunner, ask AskFunc, helo string, navigatePane func(direction int) *Session, askForSession ...func(*Session) AskFunc) error {
	if s == nil {
		return errors.New("tui: Start was given no session")
	}
	if run == nil {
		return errors.New("tui: Start was given no way to run a command")
	}

	// Pane 0 must stay on the terminal's normal screen. tcell's default
	// terminfo enters the alternate screen during Init, which hides the
	// transcript from the terminal's native scrollback. Supplying the same
	// capabilities with only the alternate-screen enter/exit sequences removed
	// keeps keyboard and resize handling while preserving the normal scrollback.
	screen, err := pane0Screen()
	if err != nil {
		return err
	}
	frame := NewFrame()
	l := &interfaceLoop{
		session: s,
		runner:  run,
		ask:     ask,
		frame:   frame,
		group:   newGroup(),
	}
	app := tview.NewApplication().SetScreen(screen).EnableMouse(s.Options().Mouse).EnablePaste(true)
	app.SetRoot(frame, true)
	l.navigatePane = navigatePane
	if len(askForSession) > 0 {
		l.askForSession = askForSession[0]
	}
	// Pasted text is always literal content and never a submit trigger,
	// however many lines it contains: embedded '\r'/'\n' bytes used to be
	// turned into KeyEnter calls here, which is the bug that let a
	// multi-line paste send partway through itself. InsertPastedText is the
	// one place that decides what a paste becomes (plain insert, or a
	// collapsed summary over the real text), so the newline-to-Enter
	// conversion that used to live in this handler is gone for good.
	frame.SetPasteHandler(func(text string) {
		l.session.Editor().InsertPastedText(text)
		l.paint()
	})
	app.SetAfterDrawFunc(func(screen tcell.Screen) {
		if l.consumeBell() {
			screen.Beep()
		}
	})
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key, r := keyEvent(event)
		if key == KeyNone {
			return nil
		}
		if l.act(ctx, key, r) {
			app.Stop()
			return nil
		}
		l.paint()
		return nil
	})
	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		// Leave wheel events to the terminal emulator. The transcript is rendered
		// in one screen and the terminal's own scrollback is the scrollback the
		// reader controls with the wheel. Keyboard /up and /down continue to use
		// the session viewport for now.
		return event, action
	})

	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		app.Stop()
		l.group.Close()
	}()
	l.paint()

	// The HELO is sent here, after the frame is first painted and before the
	// reader's keys are read, so it is in flight from the moment the screen is up
	// rather than waiting on the first line they submit. It runs through the same
	// l.start as any other turn, so a reader who presses escape while it is still
	// running stops it the same way, and leaving while it is in flight waits for it
	// through the same l.group.Close() every other turn is waited for by.
	maybeSendHELO(ctx, l, helo)

	finished := make(chan struct{})
	defer close(finished)
	go func() {
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-finished:
				return
			case <-ticker.C:
				app.QueueUpdateDraw(func() {
					now := time.Now()
					due := l.session.BreakDue(now)
					if due {
						l.session.StartBreak(now)
					} else {
						l.session.TickBreak(now)
					}
					if due && l.session.Options().BreakBell {
						l.bellPending = true
					}
					l.paint()
				})
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			app.Stop()
		case <-finished:
		}
	}()
	err = app.Run()
	if errors.Is(err, tcell.ErrNoScreen) {
		return ErrNoTerminal
	}
	return err
}

// Start runs Pane 0 as ordinary terminal output. The terminal's native scrollback owns
// history; this loop only appends complete rows and redraws the current prompt.
func Start(ctx context.Context, s *Session, run LineRunner, ask AskFunc, helo string, navigatePane func(direction int) *Session, askForSession ...func(*Session) AskFunc) error {
	if s == nil {
		return errors.New("tui: Start was given no session")
	}
	if run == nil {
		return errors.New("tui: Start was given no way to run a command")
	}
	screen, err := pane0Screen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return ErrNoTerminal
	}
	defer screen.Fini()
	screen.EnablePaste()

	l := &interfaceLoop{session: s, runner: run, ask: ask, group: newGroup(), navigatePane: navigatePane}
	if len(askForSession) > 0 {
		l.askForSession = askForSession[0]
	}
	ctx, cancel := context.WithCancel(ctx)
	defer func() { cancel(); l.group.Close() }()

	out := os.Stdout
	printed := map[*Session]int{}
	lastRow := map[*Session]string{}
	lastLive := map[*Session]string{}
	liveRows := 0    // terminal rows the live region currently occupies, not log-row count
	controlRows := 0 // terminal rows the prompt/status block currently occupies
	promptRows := 0  // terminal rows the lone prompt row currently occupies, tracked
	// separately from controlRows because a per-keystroke redraw rewrites only the
	// prompt, not the whole control block, and needs its own wrapped-row count to
	// clear correctly.
	cols := func() int {
		w, _ := screen.Size()
		return w
	}
	rowsAvail := func() int {
		_, h := screen.Size()
		return h
	}
	// drawPrompt is linePrompt plus the one piece of bookkeeping every caller of
	// it needs: controlRows is lineControls' own idea of the block's height, and
	// it is only ever refreshed when lineControls itself runs. The reader's own
	// typing between two such refreshes can wrap the prompt to more or fewer
	// rows than controlRows still assumes, and onSubmit's own lineControls call
	// trusts controlRows as the previous height to clean up after - so left out
	// of sync, it cleans up relative to a height that was never actually drawn,
	// which is what left a stray tail of the just-typed line behind once Enter
	// handed off to onSubmit.
	drawPrompt := func() {
		before := promptRows
		linePrompt(out, l.session.Editor().Text(), &promptRows, cols(), wrappedRows("orcli", cols()))
		controlRows += promptRows - before
	}
	l.onSubmit = func() {
		if sessionRunning(l.session) {
			// A turn was already running when this one was submitted (queued
			// behind it via /queue rather than starting fresh) - the control
			// block stays exactly as frozen as it was for the turn in flight,
			// which is the behavior every other running-turn path here keeps.
			return
		}
		controlRows = lineControls(out, l, cols(), controlRows)
		promptRows = 0
	}
	printRows := func() bool {
		state, _ := l.session.State()
		running := state == StateThinking || state == StateWorking
		rows := l.session.Log().Rows()
		at := printed[l.session]
		if at > len(rows) {
			at = 0
		}
		start := at
		if at == len(rows) {
			if at == 0 || len(rows) == 0 || lastRow[l.session] == rows[len(rows)-1].Text {
				return false
			}
			start = len(rows) - 1
		}
		pending := rows[start:]
		if running {
			// Every row in pending except the newest is already final.
			// ReplaceResponse re-parses a streamed response's whole
			// accumulated text on every delta, but responseParts only ever
			// splits on complete fenced code blocks, scanning left to right -
			// new text is only ever appended at the end, so every boundary it
			// already found earlier is still exactly where it was, and every
			// part before the last is byte-identical no matter how much more
			// streams in afterward. ReplaceToolStatus is narrower still: it
			// only ever revises the single row matching one tool call's own
			// ID. So pending's older rows are done changing the moment
			// anything else has been appended after them, and holding them in
			// the redraw-every-tick live region bought nothing: it just made
			// already-settled text visually flicker and reshuffle under the
			// reader on every tick, instead of scrolling up and staying put
			// the way the rest of this design's own completed-output handles
			// everything else. They are flushed here exactly like the
			// completed-output path below does, which is what lets a long
			// reply read top to bottom like any other terminal output while
			// it is still arriving, rather than only once the whole thing is
			// done.
			//
			// Only the newest row - the one still actually being written -
			// stays live, and even it has to fit the terminal: renderLiveRows
			// moves the cursor up past the whole live region to reach the
			// control block, and an ANSI cursor-up clamps at the top of the
			// screen instead of erroring, so a row taller than the terminal
			// would silently break every position this code assumes after
			// it. One oversized row cannot be shrunk by trimming rows around
			// it, since there are none left to trim once older rows are
			// already committed - the only sound choice is to show it live
			// not at all until the turn finishes and the always-safe,
			// forward-only completed flush can print it in one pass.
			if len(pending) > 1 {
				toCommit := pending[:len(pending)-1]
				clearLiveRows(out, liveRows, controlRows)
				liveRows = 0
				movePastControls(out, controlRows)
				for _, row := range toCommit {
					fmt.Fprintf(out, "%s\r\n", lineMarker(row))
				}
				printed[l.session] += len(toCommit)
				lastRow[l.session] = toCommit[len(toCommit)-1].Text
				pending = pending[len(pending)-1:]
				controlRows = lineControls(out, l, cols(), controlRows)
				promptRows = 0
			}
			budget := rowsAvail() - controlRows
			if budget < 1 {
				budget = 1
			}
			c := cols()
			live := pending
			if wrappedRows(lineMarker(pending[0]), c) > budget {
				live = nil
			}
			// The 80ms ticker calls this on a fixed cadence regardless of whether
			// the provider has sent anything new since the last tick - a still
			// loading/thinking gap, or a tool status sitting unchanged while a
			// command runs, ticks just as often as an active stream does. Without
			// this check, every one of those idle ticks still redrew the live
			// region from scratch: clear, move, rewrite, byte-identical to what
			// was already on screen. That is wasted work at best, and at worst it
			// is what read to a person as the same text "writing over itself" -
			// the exact complaint the eager-commit fix above addressed for
			// settled rows, happening again here for the one row that is still
			// legitimately live. Key on the row's text plus controlRows (not just
			// text) so a resize - which can change controlRows without changing
			// pending[0].Text - still forces the one redraw it needs.
			key := fmt.Sprintf("%d:%t:%s", controlRows, live == nil, pending[0].Text)
			if key == lastLive[l.session] {
				return false
			}
			lastLive[l.session] = key
			renderLiveRows(out, live, &liveRows, controlRows, c)
			return true
		}
		clearLiveRows(out, liveRows, controlRows)
		liveRows = 0
		movePastControls(out, controlRows)
		for _, row := range pending {
			// "\r\n" rather than Fprintln's bare "\n": this raw-mode screen does not
			// translate a line feed into a carriage return, so a row shorter than
			// the one before it would otherwise start partway across the row,
			// showing through whatever that longer row left behind at those
			// columns instead of starting clean at the left margin.
			fmt.Fprintf(out, "%s\r\n", lineMarker(row))
		}
		printed[l.session] = len(rows)
		if len(rows) > 0 {
			lastRow[l.session] = rows[len(rows)-1].Text
		}
		return true
	}

	printRows()
	controlRows = lineControls(out, l, cols(), controlRows)
	maybeSendHELO(ctx, l, helo)

	events := make(chan tcell.Event)
	go func() {
		for {
			e := screen.PollEvent()
			select {
			case events <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			now := time.Now()
			if l.session.BreakDue(now) {
				l.session.StartBreak(now)
				if l.session.Options().BreakBell {
					fmt.Fprint(out, "\a")
				}
			} else {
				l.session.TickBreak(now)
			}
			if printRows() {
				if !sessionRunning(l.session) {
					controlRows = lineControls(out, l, cols(), controlRows)
					promptRows = 0
				}
			}
		case event := <-events:
			switch e := event.(type) {
			case *tcell.EventKey:
				key, r := keyEvent(e)
				if key == KeyNone {
					continue
				}
				if l.act(ctx, key, r) {
					return nil
				}
			case *tcell.EventPaste:
				// Paste markers bracket the literal key events that follow; key handling
				// already inserts those runes into the editor.
			case *tcell.EventResize:
				// tcell delivers one of these the moment Init() finishes, reporting the
				// terminal's starting size, and again on every real resize - neither
				// carries typed input. Falling through to the generic key-handling
				// redraw below used to answer it with linePrompt, which rewrites only
				// the prompt's own row and assumes the cursor is already sitting there;
				// right after startup it is instead sitting wherever lineControls last
				// left it (its own first row, one row above the prompt), so that redraw
				// landed on the wrong row and clobbered it. A resize has nothing to type
				// into the editor, so a full control-block redraw against the new width
				// is the only thing it should ever cause.
				//
				// A turn in flight is not a reason to skip this. renderLiveRows always
				// measures the live rows' own wrapping against cols() fresh on every
				// tick, but it still trusts controlRows - the height of the block
				// above it - to say how far to jump. A resize mid-turn (dragging a
				// GUI terminal window while a reply streams, which is exactly what
				// happens in ordinary use) leaves controlRows pinned to the width it
				// was last drawn at while every live-row redraw after it measures
				// against the new one, and that mismatch does not correct itself: it
				// compounds on every single tick for as long as the turn keeps
				// streaming, which is what a growing diagonal staircase of
				// single-character rows turns out to be. Clearing the live region at
				// its old position before redrawing the control block, so the next
				// tick's renderLiveRows starts clean at the new one, is what keeps a
				// resize from ever being more than a one-time, self-correcting jump.
				if sessionRunning(l.session) {
					clearLiveRows(out, liveRows, controlRows)
					liveRows = 0
				}
				controlRows = lineControls(out, l, cols(), controlRows)
				promptRows = 0
				continue
			default:
				// An event this loop has no reaction to. Falling through to the
				// key-handling redraw below answered it anyway, for the same reason
				// *tcell.EventResize's own case above exists: that redraw assumes a key
				// or a paste just happened, and nothing here says one did.
				continue
			}
			// No generic clear here: lineControls and linePrompt both now enter and
			// leave at the same anchor (the "orcli" banner's own row) and each
			// clears exactly what it is about to rewrite, so a clear here would
			// only ever wipe content neither of them is about to repaint - which,
			// for the "orcli" text linePrompt deliberately skips over rather than
			// rewriting, means wiping it for good.
			if printRows() {
				if !sessionRunning(l.session) {
					controlRows = lineControls(out, l, cols(), controlRows)
					promptRows = 0
				} else {
					drawPrompt()
				}
			} else {
				drawPrompt()
			}
		}
	}
}

// linePrompt rewrites the prompt row in place for one keystroke, without redrawing
// the rest of the control block.
//
// The prompt and the reader's own typed text can together exceed the terminal's
// width and wrap to more than one terminal row, same as any other line here. old
// is how many terminal rows the previous call left behind: enough of them are
// cleared (fewer if the line grew, more if it shrank).
//
// bannerRows is how many terminal rows the "orcli" banner line above the prompt
// takes (wrappedRows("orcli", cols), effectively always 1). The cursor enters and
// leaves at that banner's own row - the same anchor lineControls uses - rather
// than at the prompt's row one below it: the two draw functions run back to back
// (a keystroke right after startup, or lineControls right after the reader's own
// typing on submit), and if each expected a different resting row, whichever ran
// second would clear and write starting from the wrong one, which is what used to
// leave a stray leftover row of the previous prompt behind once the two met.
func linePrompt(out *os.File, text string, old *int, cols, bannerRows int) {
	line := Prompt + text
	rows := wrappedRows(line, cols)

	cursorDown(out, bannerRows)
	fmt.Fprint(out, "\r\x1b[2K")
	fmt.Fprint(out, line)

	total := rows
	for total < *old {
		fmt.Fprint(out, "\n\r\x1b[2K")
		total++
	}
	cursorUp(out, total+bannerRows-1)
	*old = rows
}

func sessionRunning(s *Session) bool {
	state, _ := s.State()
	return state == StateThinking || state == StateWorking
}

// renderLiveRows redraws only the transient rows below the status panes. The cursor
// returns to the prompt afterward, so editing never leaves stale status lines behind.
//
// controlRows is how many terminal rows the prompt/status block above actually
// occupies right now, and cols is the terminal's current width. Both are measured
// rather than assumed: a row (the block's own lines, or a live row) that is wider
// than the terminal wraps to more than one terminal row, and a cursor jump sized by
// line count rather than by wrapped row count lands short or long, which is what let
// a live update stomp the completed stdout above it or leave stray lines behind.
func renderLiveRows(out *os.File, rows []Row, old *int, controlRows, cols int) {
	clearLiveRows(out, *old, controlRows)
	cursorDown(out, controlRows)
	fmt.Fprint(out, "\r")
	total := 0
	for _, row := range rows {
		text := lineMarker(row)
		fmt.Fprintf(out, "%s\r\n", text)
		total += wrappedRows(text, cols)
	}
	cursorUp(out, controlRows+total)
	*old = total
}

func clearLiveRows(out *os.File, n, controlRows int) {
	if n <= 0 {
		return
	}
	cursorDown(out, controlRows)
	for i := 0; i < n; i++ {
		fmt.Fprint(out, "\r\x1b[2K\n")
	}
	cursorUp(out, controlRows+n)
}

// movePastControls positions the cursor after the prompt and status panes. The
// completed rows are then written sequentially, exactly like a `cat` command: no
// already-emitted output is erased or redrawn.
//
// The trailing "\r" matters as much as moving down: a cursor move changes row,
// never column, and this terminal's raw mode does not translate a bare "\n" to a
// carriage return either, so without it the first completed row written after this
// would start at whatever column the control block's own last write left behind
// rather than at the left margin.
func movePastControls(out *os.File, controlRows int) {
	cursorDown(out, controlRows)
	fmt.Fprint(out, "\r")
}

// cursorDown and cursorUp move the cursor by exactly n rows, including n == 0,
// where they write nothing at all.
//
// A CSI cursor-movement sequence with a 0 parameter is not a no-op: ANSI treats a
// 0 (and an omitted parameter) as a request for the default count of 1, so
// "\x1b[0B" moves the cursor down one row rather than staying put. controlRows is
// legitimately 0 the first time this runs in a session (no control block has been
// drawn yet), and writing the escape unconditionally there was an extra,
// off-by-one row drop that this guards against.
func cursorDown(out *os.File, n int) {
	if n <= 0 {
		return
	}
	fmt.Fprintf(out, "\x1b[%dB", n)
}

func cursorUp(out *os.File, n int) {
	if n <= 0 {
		return
	}
	fmt.Fprintf(out, "\x1b[%dA", n)
}

// lineControls draws the prompt and the two status panes below it, and reports how
// many terminal rows the block actually took so a caller can return the cursor to
// where it started and so renderLiveRows/clearLiveRows know where the block ends.
// The block's own lines can each wrap to more than one terminal row (a long working
// directory, a long status line), so the row count is measured against the current
// terminal width rather than fixed at the block's five logical lines.
//
// prevRows is how many terminal rows the block's previous draw took - typically
// the controlRows a caller already has in hand. A long typed question wraps the
// prompt line across several terminal rows; once it is sent, this draws the
// now-empty, single-row prompt in its place, and without clearing the rest of
// what the longer draw left behind, those extra rows stay on screen as a
// permanent, meaningless row in the reader's scrollback once real output scrolls
// past them - the same leftover-row problem linePrompt already guards against
// for its own, lighter-weight redraw.
func lineControls(out *os.File, l *interfaceLoop, cols, prevRows int) int {
	state, detail := l.session.State()
	figure := "idle"
	if state == StateThinking || state == StateWorking {
		figure = twiddleWord(state)
	}
	status := l.status(state, detail, figure)
	prefix, figureText, suffix := renderBarOne(status)
	bar1 := prefix + figureText + suffix
	bar2 := renderBarTwo(status)
	promptLine := Prompt + l.session.Editor().Text()
	rows := wrappedRows("orcli", cols) + wrappedRows(promptLine, cols) + wrappedRows("", cols) +
		wrappedRows(bar1, cols) + wrappedRows(bar2, cols)
	// "\r\n" between segments, not a bare "\n": this raw-mode screen does not
	// translate a line feed into a carriage return, so each later segment would
	// otherwise start at whatever column the one before it ended on rather than
	// at the left margin - which is also what inflated the measured row count
	// above the five logical lines, since a status line starting mid-row wraps
	// one row sooner than the same text would starting at column zero.
	//
	// "\x1b[2K" before every segment, not only the first: a segment here is only
	// ever as long as what it is drawing now, never padded out to what the
	// previous draw at this row held, so a row that shrinks - the blank line,
	// once the reader's own typing had wrapped the prompt across it and across
	// the row after it, is exactly this - would otherwise still show whatever
	// that longer previous draw left on it, merely overwritten at the front by
	// however much of it the new, shorter content reaches.
	fmt.Fprintf(out, "\r\x1b[2Korcli\r\n\x1b[2K%s\r\n\x1b[2K\r\n\x1b[2K%s\r\n\x1b[2K%s\r\n", promptLine, bar1, bar2)
	total := rows
	for total < prevRows {
		fmt.Fprint(out, "\r\x1b[2K\n")
		total++
	}
	cursorUp(out, total)
	return rows
}

// wrappedRows reports how many terminal rows a line occupies once the terminal
// wraps it, given a column width. A blank line still takes one row, since that is
// what printing an empty Fprintln does. cols <= 0 (no terminal size yet) is treated
// as no wrapping, matching the one-row-per-line behavior this replaces.
//
// s is split on embedded newlines first, and each resulting line measured on its
// own, rather than measuring the whole of s as one run of display width. A Row's
// Text is allowed to hold more than one real line - log.go's own doc comment
// says a fenced code block is kept whole rather than split as it arrives - and
// measuring that as if its embedded "\n" bytes were ordinary printable
// characters first undercounts short multi-line rows and, for a row that is
// still growing (a streamed response's text is re-parsed and re-measured whole
// on every delta), produces a count that grows roughly with the total text
// rather than with how many terminal rows it actually draws. That divergence
// is what let a long streamed reply demand a cursor-up far taller than any
// real terminal, which a terminal does not error on - it clamps at row 0 - so
// everything written after that lands relative to a position the cursor never
// actually reached.
func wrappedRows(s string, cols int) int {
	lines := strings.Split(s, "\n")
	total := 0
	for _, line := range lines {
		if cols <= 0 {
			total++
			continue
		}
		w := displayWidth(line)
		if w == 0 {
			total++
			continue
		}
		total += (w + cols - 1) / cols
	}
	return total
}

// lineMarker returns a row's text with embedded newlines turned into the same
// "\r\n" every other multi-segment write in this file uses, for the reason
// each of those has: this raw-mode screen does not translate a line feed into
// a carriage return, so a bare "\n" inside a multi-line row's own text would
// leave every line after the first starting at whatever column the one before
// it ended on instead of at the left margin - the same corruption class fixed
// everywhere else a row boundary is written, just one level further in, inside
// a single row that holds more than one real line.
func lineMarker(row Row) string {
	if row.Text == "" {
		return ""
	}
	if !strings.Contains(row.Text, "\n") {
		return row.Text
	}
	return strings.ReplaceAll(row.Text, "\n", "\r\n")
}

// pane0Screen returns a terminal screen that never enters the alternate screen.
// The terminfo entry is copied so the process-wide terminal definition is not
// mutated for callers outside the TUI.
//
// TCELL_ALTSCREEN=disable has to be set too, not just EnterCA/ExitCA emptied.
// tcell's own shutdown path reads that variable directly, and when it is
// unset, finding ExitCA empty does not make it skip the alternate-screen
// cleanup - its own source says so in so many words ("only needed if ExitCA
// is empty") - it falls back to clearing the whole screen instead, on the
// theory that an empty ExitCA still means something needs restoring. There
// never was an alternate screen to restore from here, so that fallback runs
// unconditionally on every session end (/quit, or the process simply
// exiting) and wipes out exactly the scrollback-preserving transcript this
// whole design exists to protect. The environment variable is the one signal
// that actually turns off tcell's alternate-screen bookkeeping end to end,
// entry and exit alike, rather than just the two terminfo capabilities that
// happen to drive its entry half.
func pane0Screen() (tcell.Screen, error) {
	if err := os.Setenv("TCELL_ALTSCREEN", "disable"); err != nil {
		return nil, err
	}
	name := os.Getenv("TERM")
	ti, err := tcell.LookupTerminfo(name)
	if err != nil {
		return nil, err
	}
	copy := *ti
	copy.EnterCA = ""
	copy.ExitCA = ""
	return tcell.NewTerminfoScreenFromTtyTerminfo(nil, &copy)
}

// maybeSendHELO starts the one turn Start sends on its own, with no key pressed
// and no line runner consulted: the capability and introduction message a reader
// would otherwise only see once they had typed something.
//
// An empty helo, or a loop built with no ask at all, sends nothing - a caller's
// choice not to greet, read here rather than decided here.
//
// It starts the turn silent: the HELO's own text is an instruction to the model,
// not a line the reader typed, so it is sent but never written into the log as a
// question row. Only the model's reply - the actual greeting - lands in the log, the
// same way any other turn's reply does.
func maybeSendHELO(ctx context.Context, l *interfaceLoop, helo string) {
	if helo == "" || l.ask == nil {
		return
	}
	l.start(ctx, helo, true)
}

// interfaceLoop is the state one run of the interface carries.
//
// It is a value rather than a set of parameters threaded through paint, because the painter,
// the key handler and the turn all need the same things and a function carrying them all is
// a function whose signature nobody can read. It is not named Loop because that reads as
// an exported thing this package offers, and it is not.
type interfaceLoop struct {
	session       *Session
	runner        LineRunner
	ask           AskFunc
	frame         *Frame
	navigatePane  func(direction int) *Session
	askForSession func(*Session) AskFunc
	prefixPending bool

	// onSubmit, when set, is called synchronously from submit() right after the
	// editor is reset and before any turn is started - see submit()'s own call
	// site for why that moment matters. Start (the line-oriented Pane 0 loop)
	// is the only caller that sets it; the full-screen tview loop never does,
	// since it redraws everything every paint() and has no equivalent staleness
	// to correct.
	onSubmit func()

	// group counts the goroutines writing to the frame, and is what leaving waits for.
	group *group

	// step is the sweep's position, advanced on each paint while a turn runs.
	step int

	// bellPending reports that a screen break just started with its bell on,
	// and the next draw owes a ring. It is touched only from the application's
	// own event-loop goroutine, by the QueueUpdateDraw callback that sets it
	// and the AfterDrawFunc that reads and clears it, so it carries no lock of
	// its own.
	bellPending bool

	// frozenStatus and haveFrozenStatus hold the last status() computed before
	// copy mode turned on. While copy mode is on, status() returns this value
	// unchanged instead of recomputing the stat fields, so the footer a reader
	// is mid-selection over does not move under their drag. It is populated the
	// first time status() runs with copy mode on, from whatever status() would
	// have returned a moment before - in practice the previous paint's figure -
	// and cleared the next time copy mode turns off, so the field resumes
	// tracking live state the instant the reader turns it back off.
	frozenStatus     Status
	haveFrozenStatus bool
	streamStart      int
	bufferActive     bool
}

// consumeBell reports whether a bell is owed for the draw about to happen,
// and clears the request so the same break does not ring it twice.
func (l *interfaceLoop) consumeBell() bool {
	pending := l.bellPending
	l.bellPending = false
	return pending
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
	palette := paletteOf(l.session)
	rows := l.session.Log().Rows()

	state, detail := l.session.State()

	// running is whether a turn is in flight, and is read once here so the figure field and
	// the state field are decided by one value rather than by two switches that have to
	// agree. The two states are the only ones a turn is in, so the word is the condition
	// rather than a field the session has to keep up to date.
	running := state == StateThinking || state == StateWorking
	copyMode := l.session.Options().CopyMode

	figure := "idle"
	if running {
		if !copyMode {
			// Copy mode freezes the sweep as well as the figure it turns on:
			// l.step stops advancing, so SetSweepStep below keeps drawing the
			// same frame of the sweep instead of recoloring it under a
			// reader's terminal-native selection.
			l.step++
		}
		// The HELO replaces thinking/working with its own word for the whole of
		// its one turn: "thinking" describes a model reasoning about a
		// question, and the HELO is not that, it is the client proving its
		// credential and model for the first time. HELOInFlight is false for
		// every other turn a session ever runs, so this never reaches past the
		// one exchange it is for.
		if l.session.HELOInFlight() {
			figure = "Connecting..."
		} else {
			figure = twiddleWord(state)
		}
	} else if !copyMode {
		l.step = 0
	}

	footer := Bar{
		Status: l.status(state, detail, figure),
		Field:  l.fieldRow(),
	}
	l.frame.SetContent(footer, rows, palette)
	if l.bufferActive && running {
		start := l.streamStart
		if start < 0 || start > len(rows) {
			start = len(rows)
		}
		buffer := make([]Row, 0, len(rows)-start+1)
		if len(rows) > 0 && isProgramBanner(rows[0]) {
			buffer = append(buffer, rows[0])
		} else {
			buffer = append(buffer, Row{Kind: KindNotice, Text: "orcli"})
		}
		buffer = append(buffer, rows[start:]...)
		l.frame.SetBuffer(buffer)
	} else {
		l.bufferActive = false
		banner := Row{Kind: KindNotice, Text: "orcli"}
		if len(rows) > 0 && isProgramBanner(rows[0]) {
			banner = rows[0]
		}
		l.frame.SetBuffer([]Row{banner})
	}
	l.frame.SetCaret(l.session.Editor().Caret())
	l.frame.SetSweepStep(l.step)
	l.frame.SetScroll(l.session.ScrollOffset())
}

func (l *interfaceLoop) fieldRow() string { return l.session.Editor().Text() }

func keyEvent(event *tcell.EventKey) (Key, rune) {
	switch event.Key() {
	case tcell.KeyRune:
		return KeyRune, event.Rune()
	case tcell.KeyEnter:
		return KeyEnter, 0
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return KeyBackspace, 0
	case tcell.KeyDelete:
		return KeyDelete, 0
	case tcell.KeyLeft:
		return KeyLeft, 0
	case tcell.KeyRight:
		return KeyRight, 0
	case tcell.KeyUp:
		if event.Modifiers()&tcell.ModShift != 0 {
			return KeyScrollUp, 0
		}
		return KeyUp, 0
	case tcell.KeyDown:
		if event.Modifiers()&tcell.ModShift != 0 {
			return KeyScrollDown, 0
		}
		return KeyDown, 0
	case tcell.KeyPgUp:
		if event.Modifiers()&tcell.ModShift != 0 {
			return KeyScrollPageUp, 0
		}
		return KeyNone, 0
	case tcell.KeyPgDn:
		if event.Modifiers()&tcell.ModShift != 0 {
			return KeyScrollPageDown, 0
		}
		return KeyNone, 0
	case tcell.KeyHome:
		return KeyHome, 0
	case tcell.KeyEnd:
		return KeyEnd, 0
	case tcell.KeyTab:
		return KeyTab, 0
	case tcell.KeyEscape:
		return KeyEscape, 0
	case tcell.KeyCtrlC:
		return KeyCtrlC, 0
	case tcell.KeyCtrlD:
		return KeyEOF, 0
	case tcell.KeyCtrlA:
		return KeyCtrlA, 0
	case tcell.KeyCtrlW:
		return KeyCtrlW, 0
	case tcell.KeyCtrlU:
		return KeyCtrlU, 0
	case tcell.KeyCtrlT:
		return KeyCtrlT, 0
	case tcell.KeyCtrlB:
		return KeyCtrlB, 0
	default:
		return KeyNone, 0
	}
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

	if !opts.CopyMode {
		l.haveFrozenStatus = false
	} else if l.haveFrozenStatus {
		// Copy mode is on and a status was already computed since it turned
		// on: hand back that same value unchanged rather than recomputing any
		// field, so the footer a reader is dragging a selection across does
		// not move under them.
		return l.frozenStatus
	}

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
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unavailable"
	}
	s[fieldHost] = host
	metrics := l.session.Usage()
	s[fieldCredits] = metrics.Credits
	if s[fieldCredits] == "" {
		s[fieldCredits] = "unavailable"
	}
	s[fieldCost] = "unavailable"
	s[fieldInput] = "unavailable"
	s[fieldOutput] = "unavailable"
	if metrics.HasInput {
		s[fieldInput] = strconv.Itoa(metrics.InputTokens)
	}
	if metrics.HasOutput {
		s[fieldOutput] = strconv.Itoa(metrics.OutputTokens)
	}
	if metrics.HasCost {
		s[fieldCost] = fmt.Sprintf("$%.4f", metrics.CostUSD)
	}
	s[fieldContext] = metrics.Context
	if s[fieldContext] == "" {
		s[fieldContext] = "unavailable"
	}
	s[fieldAutosave] = onOff(metrics.Autosave, "on", "off")
	s[fieldStealth] = onOff(opts.Cognito, "on", "off")
	s[fieldVerbosity] = strconv.Itoa(opts.Verbosity)
	s[fieldPreset] = orNone(l.session.Preset())
	s[fieldCognito] = onOff(opts.Cognito, "on", "off")
	s[fieldColor] = onOff(opts.Color, "on", "off")
	s[fieldMouse] = onOff(opts.Mouse, "on", "off")
	s[fieldCopy] = onOff(false, "available", "none")
	s[fieldBell] = onOff(opts.Bell, "on", "off")
	s[fieldPane] = l.session.Pane()
	s[fieldPaneState] = l.session.PaneState()
	s[fieldWorkers] = plural(len(l.session.Workers()), "worker", "workers")
	s[fieldQueue] = plural(l.session.QueueLen(), "prompt", "prompts")
	s[fieldHeld] = plural(log.Len(), "row", "rows")
	s[fieldFolded] = plural(log.Folded(), "row", "rows")
	s[fieldLevels] = plural(len(l.session.Levels()), "level", "levels")

	if opts.CopyMode {
		l.frozenStatus = s
		l.haveFrozenStatus = true
	}

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
	// A mouse report is not the reader typing - see the KeyMouse case below -
	// so it does not count as the activity a running screen break watches for.
	if key != KeyMouse {
		l.session.NoteBreakActivity()
	}
	if l.prefixPending {
		l.prefixPending = false
		switch {
		case key == KeyCtrlB:
			l.session.Editor().Insert('\x02')
		case key == KeyRune && (r == 'n' || r == 'N'):
			l.changePane(1)
		case key == KeyRune && (r == 'p' || r == 'P'):
			l.changePane(-1)
		}
		return false
	}
	if key == KeyCtrlB {
		l.prefixPending = true
		return false
	}

	switch key {
	case KeyRune:
		l.session.Editor().Insert(r)

	case KeyBackspace:
		l.session.Editor().Backspace()

	case KeyDelete:
		l.session.Editor().Delete()

	case KeyLeft:
		l.session.Editor().Left()

	case KeyRight:
		l.session.Editor().Right()

	case KeyHome:
		l.session.Editor().Home()

	case KeyEnd:
		l.session.Editor().End()

	case KeyTab:
		l.session.Editor().Complete()

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

	case KeyUp:
		// adr-0000011's history walk: one entry per press, stopping rather than wrapping
		// at the oldest end. The current field is handed in so the first press of a walk
		// can remember it as the draft Down eventually returns to.
		if text, moved := l.session.History().Up(l.session.Editor().Text()); moved {
			l.session.Editor().SetText(text)
		}

	case KeyDown:
		if text, moved := l.session.History().Down(l.session.Editor().Text()); moved {
			l.session.Editor().SetText(text)
		}

	case KeyCtrlA:
		l.session.Editor().Home()

	case KeyCtrlW:
		l.session.Editor().EraseWordBefore()

	case KeyCtrlU:
		l.session.Editor().ClearLeft()

	case KeyCtrlT:
		// PLACEHOLDER BINDING: Ctrl+T is a tentative choice for
		// expand/collapse of a pasted block, picked only because it did
		// not collide with Ctrl+A/Ctrl+W/Ctrl+U or anything else keyEvent
		// already mapped. Glen has not confirmed this key; it is wired
		// here so the mechanism (Editor.ToggleExpandPastedBlock) has
		// somewhere to be reached from while the real binding is decided.
		l.session.Editor().ToggleExpandPastedBlock()

	case KeyScrollUp:
		l.session.ScrollUp(1)

	case KeyScrollDown:
		l.session.ScrollDown(1)

	case KeyScrollPageUp:
		l.session.ScrollUp(l.frame.ScrollPageSize())

	case KeyScrollPageDown:
		l.session.ScrollDown(l.frame.ScrollPageSize())
	}

	return false
}

func (l *interfaceLoop) changePane(direction int) {
	if l.navigatePane == nil {
		return
	}
	if next := l.navigatePane(direction); next != nil {
		l.session = next
	}
}

// submit acts on a line the reader finished typing.
//
// A line is one of two things, and they are told apart before either is run. A
// command (IsCommand says so) goes to l.runner, which is cmd/orcli/dispatch.go's
// Run: it resolves the name against the command table and reports a Result, never
// a question for the model on its own. Everything else is a plain question, and
// this is the one place that sends it to the model - not the dispatcher, whose own
// doc comment on Run says plainly that a line which is not a command is not its
// business. A result's own Result.Ask field is still honored afterward, since a
// command can itself decide to ask the model something (the Cloudflare-not-set-up
// guidance in cmd/orcli/dispatch.go is one such case); that is a second, narrower
// reason to ask and does not make the dispatcher the place a reader's own typed
// question goes.
func (l *interfaceLoop) submit(ctx context.Context) bool {
	// SubmitText rather than Text: a still-collapsed pasted block is
	// substituted back to its real, raw text here, so the runner and the
	// history both get what the reader actually pasted, never the
	// "```pasted, N lines```" placeholder the field was showing.
	line := strings.TrimSpace(l.session.Editor().SubmitText())
	if line == "" {
		return false
	}
	l.session.Editor().Reset()

	// The reader's own typing, between here and the last full control-block
	// redraw, used only the lightweight per-keystroke prompt-only path (see
	// linePrompt), which never touches controlRows - so if what they typed
	// wrapped the prompt to a different number of terminal rows than the last
	// full redraw measured, controlRows is now stale relative to what is
	// actually on screen. The editor was just reset to empty above, on this
	// same goroutine and strictly before the turn's own goroutine can start
	// touching the session, so refreshing the control block now - while the
	// callback can still see that empty prompt, deterministically rather than
	// racing the turn's state-flipping goroutine - re-syncs it before any
	// live-row rendering for this turn ever has to consult it.
	if l.onSubmit != nil {
		l.onSubmit()
	}

	// Recorded here, where a line is actually sent, rather than where it was typed.
	// There is no message-queue mechanism in this tree yet, so this is the only
	// moment that exists: a future queue would still record at the point it drains
	// into a send, not at the point a reader queued it (adr-0000011). A plain
	// question is recorded exactly as a command is: Up/Down walks a reader's own
	// typed lines regardless of which kind each one was.
	l.session.History().Record(line)

	if _, _, ok := IsCommand(line); !ok {
		if l.ask == nil {
			// The same situation Session.Ready reports for a turn with no model:
			// there is nothing to send this to, and the reader is told so as a
			// notice rather than having the line discarded with no trace of it.
			l.session.Notice(ErrNoModel.Error(), 0, RoleFailure)
			return false
		}
		l.start(ctx, line, false)
		return false
	}

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
		l.start(ctx, result.Ask, false)
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
func (l *interfaceLoop) start(ctx context.Context, question string, silent bool) {
	l.startOnSession(ctx, l.session, question, silent)
}

func (l *interfaceLoop) startOnSession(ctx context.Context, session *Session, question string, silent bool) {
	if session == l.session {
		l.streamStart = session.Log().Len()
		l.bufferActive = true
	}
	if err := l.group.Add(); err != nil {
		session.Notice(err.Error(), 0, RoleFailure)
		return
	}
	task := l.ask
	if l.askForSession != nil {
		task = l.askForSession(session)
	}

	turnCtx, cancel := context.WithCancel(ctx)

	session.SetCancel(cancel)

	go func() {
		// The count is lowered on every path out, including a panic, since a turn that took
		// the process down does not need the count but a reader who came back would have
		// waited for it for ever.
		defer l.group.Done()

		err := task(turnCtx, question, 0, silent)

		session.ClearCancel()
		cancel()

		if err != nil {
			session.Notice(err.Error(), 0, RoleFailure)
			return
		}

		// A queued prompt is sent only once the turn ahead of it has finished with
		// no error, never after one that failed or was stopped. `/queue` is a
		// follow-up on work that went well; chaining it onto a turn the reader
		// just watched fail would send a second request behind a first one they
		// may want to look at or retype first, which is a judgment call this
		// comment flags rather than one settled by a design record: there is no
		// existing precedent in this tree for when a queued message should fire.
		if next, ok := session.Drain(); ok {
			l.startOnSession(ctx, session, next, false)
		}
	}()
}

// stop ends the turn in flight, and says so only if there was one.
//
// It is the escape handler and the only place a cancel is called, so a reader who presses it
// twice does not reach a cancel that has already fired.
func (l *interfaceLoop) stop() {
	cancel := l.session.StopTurn()

	if cancel == nil {
		return
	}
	cancel()
	l.session.Finished("stopped")
}

// paletteOf returns the palette a session draws with.
//
// It is built at draw time from the session's own options rather than held on the session,
// so a colour changed by a command takes effect on the next row rather than on the next
// session. A session with colour off writes no palette sequence at all, which falls out of
// Palette.Sequence returning nothing rather than out of a check here.
func paletteOf(s *Session) Palette {
	opts := s.Options()
	p := NewPalette(opts.Color, GroundAuto, nil)
	return p.WithPaneColors(opts.PaneActiveColor, opts.PaneDoneColor)
}
