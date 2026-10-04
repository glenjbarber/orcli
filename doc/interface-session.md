# The session, the log, the levels and the workers

What a command manipulates and what `/copy N` reaches.

Code: `internal/tui/session.go`, `internal/tui/log.go`, `internal/tui/levels.go`,
`internal/tui/worker.go`.

## The session is small on purpose

```go
type Session struct {
    log     Log
    levels  *levels
    opts    Options
    workers []*Worker
    mu      sync.RWMutex
    state   State
    detail  string
}
```

The log, the levels, the workers, the state, and the counters the frame draws.
Everything with a decision in it, the terminal control, the line editor, the
palette and the command table, is a separate concern with its own file. A session
that grew all of them would be the one file that decides everything.

`New` appends one row, the banner, so a session opening onto an empty screen gives
a reader something to tell it started.

## The four states

```go
const (
    StateIdle     State = "idle"
    StateThinking State = "thinking"
    StateWorking  State = "working"
    StatePaused   State = "paused"
)
```

Four named rather than a pair of booleans, since a reader needs to know which one
they are in and two booleans answer that in two places. `thinking` is the window
between a turn starting and its first text arriving, which is the only time a
reader would otherwise have no evidence anything is happening.

`SetState` does not check the transition, and the comment records why: a session
that refuses a transition it did not expect has to hold the state somewhere to
refuse it, and that somewhere is the place the caller already wrote. The rule that
matters is enforced by the caller, that nothing may claim idle while a turn runs.

`Begin` and `Deliver` are what move through them. `Begin` writes the question as a
row and sets thinking. The first `Deliver` sets working. `Finished` writes a row
carrying the reason and sets idle.

## The log

```go
const LogBound = 20000
```

A bound and not a limit on the reader. A session left running for a day should not
hold every tool call it ever made, and the rows it drops are what the frame's
count field reports as `fieldFolded`.

`Append` drops whole rows from the front rather than cutting one in half, since a
row cut at the top has an unreadable beginning. `Truncate` folds everything it held
into the same counter, so `/clear` empties the visible transcript without
pretending the session did nothing.

**`Rows()` copies the slice, not the rows.** A caller reads it while a turn
appends without racing. The rows themselves are not copied, which is safe because
a row is written once and never rewritten: an append that trims moves the slice
header and does not touch a row still in it.

### A row

```go
type Row struct {
    Text  string
    Spans []Span
    Level int
    Kind  RowKind
}
```

Text and styling beside each other, never styling inside the text. The rule is that
a row can be measured, cut, searched and copied without knowing anything about
colour, and that a model cannot end the frame by writing an escape into a reply.

`Kind` is one of six values and is attached when the row is written:

```go
KindReply, KindQuestion, KindTool, KindQueued, KindNotice, KindChrome
```

It is a value and never a string carrying a prefix, since a model can write
`[shell]` at the start of a sentence as easily as the client can.

`Level` is attached at write time for the same reason. A level looked up from pane
state at draw time would re-attribute a row the reader had already read to a
different responder.

### The filter

```go
func PlainRow(row Row) Row
```

`plainRow` drops the C0 and C1 controls and the delete character, keeping only the
tab. Whole sequences are removed rather than only the control byte: a
clear-screen that lost only its ESC arrives at the reader as the letters `2J`,
which is visible nonsense rather than nothing at all.

`escapeLength` reports how many runes past an escape belong to its sequence, and
covers three shapes: CSI running to a final byte in 0x40 to 0x7e, string sequences
running to BEL or ST, and everything else as two bytes or the escape alone.

A sequence with no terminator takes the rest of the row rather than the whole log,
since one row the endpoint wrote badly should cost the reader that row.

## The levels

```go
type Level struct {
    Number int
    Parent int
    Title  string
    Closed bool
}
```

**A level is a thread identity, not a pane identity.** A pane could change while a
turn ran and a row already written would then be re-attributed. A level is fixed
when the row is written and never moves.

**A level is retired, never reused.** Reuse is what makes a handle lie: a reader
who copied level 4 an hour ago and types `/copy 4` now would get whatever took the
number, with nothing to say so. A retired level keeps what it was, so `/copy 3`
after 3 closed can say what 3 was. That is the difference between a reader who
mistyped and a reader who is behind.

Level 0 is the conversation and cannot be closed. `rootParent` is -1 rather than 0
because 0 is a real level, and a level whose parent reads as its own is a cycle in
anything that walks the tree.

`OpenLevel` allocates the number when the level is opened, not when the thread is
asked, so a reader who walks away from a thread leaves a hole that is visible in
the list rather than a number that could be handed out twice.

### Three errors, kept apart

```go
var ErrNoLevel     = errors.New("there is no such level")
var ErrLevelClosed = errors.New("that level has been closed")
var ErrRootLevel   = errors.New("that level is the conversation and cannot be closed")
```

A number that was never handed out and a number that has been retired are two
different situations, and telling a reader the same thing for both makes the
handle untrustworthy. `CopyLevel` and `CopyText` return the rows alongside
`ErrLevelClosed` rather than refusing, so the reader gets their text and learns the
level is closed.

## The workers

```go
type Worker struct {
    Level    Level
    Question string
    state    WorkerState
    cancel   func()
}
```

**A worker is a level.** It is already a thread: its own conversation, its own log
rather than the main one, outliving the turn that started it. Without a level it
would be background work a reader can watch but cannot copy out of, and a worker
whose findings have to be selected by hand is a worker whose findings do not get
used.

Three states, and the third covers two situations:

```go
WorkerRunning  // started, not finished
WorkerDone     // finished on its own
WorkerStopped  // stopped by the reader, or failed
```

Stopped is one value rather than two because a reader who stopped a worker and a
worker that broke are both a worker that is not running, and the difference belongs
in the row that said why rather than in a state a reader scans.

**A worker is refused under cognito.** Cognito promises nothing is recorded, and a
worker runs programs and writes files. A file it wrote is a record of what the
model did even though the transcript records nothing.

```go
func (s *Session) Spawn(parent int, question string) (*Worker, error)
func (s *Session) SpawnWithCancel(parent int, question string, cancel func()) (*Worker, error)
```

The parent is a parameter rather than the session's current level, since a reader
deep in a `/btw` wants a worker to inherit that prefix rather than the root.

`SpawnWithCancel` is a second entry point rather than a field the caller sets after,
because the cancel has to exist before the worker runs or the first stop is a stop
of nothing. A window in which a reader cannot stop a model is the same failure as
never being able to.

`Finish` and `Stop` are safe to call twice. A reader who presses the key twice is
asking twice, and the second ask failing is a failure they did not cause.

**A worker's state does not change the session's.** Folding them together would
mean a background worker making the interface look busy, which is a lie about the
reader's own turn.

## Copying

```go
func (s *Session) CopyText(n int) (string, Level, error)
func (s *Session) CopyLevel(n int) ([]Row, error)
```

The shape is the decision rather than the encoding: every row at the level, in the
order they happened. A worker copied out of the session is a question and the
answer to it, and a reader pasting that into a ticket wants both rather than an
answer with no question attached.

A row keeps its text and loses its styling, since a clipboard is plain text and a
span is a thing this package means to itself. The credential is in no row, so it is
in no copy, and that is the reason the log holds a plain-text contract rather than
one that is only plain while nothing interesting has happened.

## Known gaps

1. **A worker has no tool loop in this branch.** `Spawn` allocates the level and
   writes the question row, and `Finish` retires it. Nothing runs the turn. The
   loop described in `SPAWN-API.md` is not built here.

2. **No conversation is held.** `Session` has no field for messages, and
   `Begin` returns the context without recording a turn. What a turn carries is
   left open on purpose, since it has not been decided.

3. **`RowsAt` copies every row it finds.** A level with many rows builds a slice of
   all of them, and `CopyText` builds the whole string. Neither is bounded by
   `LogBound` beyond what the log already trimmed.