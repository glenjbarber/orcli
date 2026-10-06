# The command table and the exported surface

The table, the dispatcher, completion, the line editor, and what a caller outside
`internal` can reach.

Code: `internal/tui/command.go`, `internal/tui/command_line.go`,
`internal/tui/line.go`, `internal/tui/alias.go`, `internal/tui/diff.go`,
`internal/tui/width.go`, `cmd/orcli/dispatch.go`, `cmd/orcli/confirm.go`.

What the merge that brought this tree up to date changed is recorded in
`staged/adr-0000003-first-request-is-the-probe.txt` and
`staged/adr-0000004-model-choice-records-replaced.txt`, and the short of it is here
rather than left to a reader who has to go looking.

## The table is the one place names are written

```go
type Command struct {
    Name     string
    Summary  string
    Aliases  []string
    Hidden   []string
    Args     string
    IdleOnly bool
}
```

Four consumers read it and nothing else declares a name: the dispatcher, the help,
the completer, and the tests.

**It is filled in `init`, not in a variable initialiser,** and the comment records
both reasons. The help renderer reaches this table, so a variable initialiser
reaching the help would form an initialisation cycle that Go reports at compile
time. And a package-level initialiser runs before `init`, so a `byName` written as
one would be built from an empty table and every lookup would miss. The second
produces a silently empty table rather than a build failure, which is why it is
worth writing down.

## The table, in declaration order

| Name | Args | Idle only | Summary |
| --- | --- | --- | --- |
| `help` | | | list this table |
| `version` | | | print the version |
| `key` | | | report the usage against the key |
| `search` | TEXT | | search the log, filtered as it is typed |
| `models` | TEXT | | list the models, filtered as it is typed |
| `freemodels` | TEXT | | list the models that cost nothing, filtered as typed |
| `model` | NAME | | show or choose the model, without an argument to list |
| `attribute` | NAME | | set the model this session answers with, from the ones offered |
| `new` | | yes | clear the conversation |
| `bell` | | | ring the terminal bell on reply, on or off |
| `color` | | | turn color on or off, and save the choice |
| `cognito` | | | record nothing, on or off |
| `verbosity` | 0-5 | | how much the model is asked to answer with |
| `verbose` | | | report the shape of each streamed turn, on or off |
| `delegate` | QUESTION | | ask a question alongside, without recording it |
| `pane` | main\|delegate\|spawn | | show the conversation, the delegate output or the spawn output |
| `spawn` | QUESTION | | answer a question in a worker given the tools |
| `btw` | QUESTION | | start a thread branched from this conversation |
| `close` | N | | dereference the level a copy would name |
| `queue` | TEXT | | add a follow-up prompt to the queue |
| `redirect` | | | interrupt and redirect the prompt |
| `main` | | yes | leave the thread and return to the conversation |
| `compact` | | yes | summarise the conversation and start again |
| `save` | NAME | | write the conversation to a file of its own |
| `load` | NAME | | resume a conversation saved with /name |
| `mouse` | | | turn mouse reporting on or off, for wheel scrolling |
| `pause` | | | stop what the client writes, and toggle the mouse |
| `clear` | | | clear the log |
| `info` | | | report the session settings |
| `copy` | N | | copy the level N, or the whole conversation |
| `permission` | add\|remove DIR PROG... | | grant or refuse programs in a directory |
| `autosave` | on\|off\|now | | write the conversation without being asked |
| `approve` | ask\|allow\|refuse | yes | report or set whether programs run without asking |
| `tools` | | | list the tools the model is given, and the root they are in |
| `test` | DIR | | list a directory, for testing how the frame draws |
| `quit` | | | leave the interface |
| `exit` | | | leave the interface |
| `cloudflare` | | | the Cloudflare commands |

**There is no `connect`.** It was in the table with no handler, and 0000003 removed
it: a reader's own first question is the probe, and a turn that came back with
text is what proves the credential and the model together.

Two entries carry a hidden name or an alias, and the reasons are in the source.

`color` has `Hidden: []string{"colour"}`. It resolves through `Lookup`, and is
neither listed in the help nor offered by the completer. It is a compatibility
surface rather than a feature, and listing it would spend a line of help on a
spelling nobody asked for.

`attribute` has `Aliases: []string{"attribution"}`. The two are not one command:
`/model` shows or chooses and is the reader's everyday word, while `/attribute`
says the value has to be one the endpoint offers. Merging them would take that
decision from the reader.

**The table lists thirty-nine entries and this build runs four.** The dispatcher in
`cmd/orcli` implements `cloudflare`, `model`, `test` and `quit`. Everything else is
reported as listed-but-not-run, which is a different message from unknown, since a
reader told there is no such command goes looking for a typo they do not have.

## The dispatcher

```go
func (d *dispatcher) Run(ctx context.Context, line string) (tui.Result, error)
```

```go
name, args, ok := tui.IsCommand(line)
if !ok {
    return tui.Result{}, nil
}

h, known := d.commands[name]
if !known {
    if _, listed := tui.Lookup(name); listed {
        return tui.Result{}, fmt.Errorf("/%s is in the table but this build does not run it yet", name)
    }
    return tui.Result{}, fmt.Errorf("unknown command /%s", name)
}
```

The two refusals are told apart deliberately.

**A line that is not a command returns an empty result and no error.** The loop
sends a question to the model and a command to the dispatcher, and a dispatcher
that also answered questions would be two things deciding what a line means.

**The handler type takes the dispatcher** rather than reaching for one through a
package variable, so two dispatchers in one test run cannot reach each other's
held proposal.

### Result

```go
type Result struct {
    Text string
    Ask  string
    Quit bool
}
```

A small type rather than a string and an error because a line can end up meaning
two different things. `Text` is what the reader is shown, `Ask` is a question for
the model, and `Quit` asks the loop to leave, which only `/quit` sets.

**A newline in `Text` is a row break.** The loop splits on it and writes one row
per line, so a command answering with a listing gets a listing rather than one row
with the lines run together. The split is in `submit` rather than in each handler,
because the loop owns the log. A carriage return beside a newline is one end and
not two, which is how a program writing to a terminal ends a line.

## `/model`, and the connection it replaced

```go
func modelHandler(s *tui.Session, args string) (tui.Result, error)
```

Four cases, and they are told apart before anything is written:

| Argument | What it does |
| --- | --- |
| none | reports the model in force and the one `/model last` would reach |
| `last` | swaps the two members, so the reader lands where they started |
| a name | records the model in force under `last_model` and writes the new one |
| a name, with nothing in force | writes it and leaves `last_model` alone |

**Two `last` commands are a no-op.** The swap is its own inverse, so pressing it
twice puts both members back. `TestLastTwiceIsANoOp` holds it by comparing the
file byte for byte.

**A `last` with nothing to go back to is refused by name.** An empty model is a
session that cannot ask anything, and one the reader did not choose.

**A choice moves the session as well as the file**, through `Session.SetModel`, so
the frame does not draw one model while the reader is answered by another. The
setter takes the lock the state already takes, since `ask` reads the model on the
request goroutine.

**A turn that came back with text is the confirmation, and a confirmation does not
record the model it replaced.** A reader's first question is a probe rather than a
move, and treating it as one would send `/model last` to a model they never chose.
The confirmation writes once per session and a failed write is a row in the log
rather than a refusal, since the turn was answered and the answer is the thing the
reader asked for.

Three seams carry it, all in `cmd/orcli`, and all three are substituted by every
test: `configPath`, `writeModel` and `writeModelSwap`. Without them a test proving
a model was written would read the reader's own configuration file.

## The input loop

```go
func Start(ctx context.Context, s *Session, screen *Screen, run LineRunner, ask AskFunc) error

type LineRunner func(ctx context.Context, line string) (Result, error)
type AskFunc    func(ctx context.Context, question string, level int) error
```

Both are passed in rather than reached for, so this package keeps no credential
and no client.

### The keys

| Key | Effect |
| --- | --- |
| rune | insert |
| Backspace, Delete | remove |
| Left, Right, Home, End | move the caret |
| Tab | complete |
| Enter | submit |
| Escape | stop the turn, leave the field alone |
| Ctrl-C | stop the turn and leave |
| EOF | leave |
| mouse report | consumed, nothing acts on it |
| up, down | read, no history in this unit |

**Ctrl-C reaches the loop rather than raising a signal,** since ISIG is cleared on
entry, and it is the reader saying leave, which is what a shell would do with it.

**Escape is the only interrupt trigger.** It stops the turn and leaves the field
alone, since a reader who interrupts a running turn has not said they want to
abandon what they were typing.

**A mouse report is consumed and nothing acts on it.** Consuming it is what keeps
its bytes out of the field.

**The turn count is raised before the goroutine and lowered after the answer has
been drawn,** so a reader who leaves while a turn is in flight waits for it rather
than handing the terminal back with a request still writing to it. A count raised
after leaving began waiting is a count nobody is waiting for, so the group refuses
one rather than accepting it silently.

## Completion

```go
func Names() []string
func Complete(prefix string) (names []string, whole bool)
func Completion(prefix string) (text string, caret int, whole bool)
```

**`Names` is alphabetical rather than table order,** since a completer whose order
is an accident of the source file is a completer whose order is not a decision.

**Hidden names are not offered,** since a name that is never listed and never
completed is what hidden means.

**The trailing space in `Completion` is the whole point.** It separates a finished
name from the argument that follows, and without it a name that takes an argument
is indistinguishable from the end of the name: the reader cannot tell whether to
type a space or whether the command takes nothing after it.

**It is returned rather than applied.** The line editor owns the field and the
caret, and a completer writing into the terminal behind it would be two things
writing to one place.

**An ambiguous prefix is left alone.** The field is what the reader typed, and a
completer offering a choice would be a prompt this interface does not have.

**`IsCommand` strips the slash and cuts at the first space,** so `name` arrives
without it and a line that is only a slash is not a command at all.

## Aliases

A saved alias is a reader's own name for a command with its arguments fixed.

```go
func SetAlias(name, invokes, args string) error
func RemoveAlias(name string) bool
func AliasNames() []string
func AliasSummary(name string) (string, bool)
func IsSavedAlias(name string) bool
func Resolve(name string) (invokes, args string, saved, found bool)
```

**`Resolve` reports both halves of where a name came from.** `saved` says whether
the name is the reader's own rather than the table's, and `found` whether anything
answered at all, so a caller can tell a mistyped alias from a mistyped command.

**`AliasSummary` returns a description rather than a command,** since an alias is
not a table entry and the help that renders it cannot reach the table.

## The line editor

`Editor` is a value over the field and never writes to the terminal. The painter
asks it what the prompt row should say.

```go
func NewEditor() Editor
func (e Editor) Text() string
func (e Editor) Caret() int
func (e Editor) Empty() bool
func (e *Editor) Reset()
func (e *Editor) Insert(r rune)
func (e *Editor) Backspace()
func (e *Editor) Delete()
func (e *Editor) Left() / Right() / Home() / End()
func (e *Editor) ClearLeft() / ClearRight()
func (e *Editor) Complete()
```

**The caret is a rune index, not a byte offset,** so a byte offset into the middle
of a multi-byte character is a slice bound that panics rather than a position that
is merely wrong.

**A key at the boundary does nothing.** Backspace at the start and Delete at the
end are refused rather than made to remove a character the field does not have,
since a field that lost a character it does not have is a field the reader has to
notice and undo.

**`Complete` completes one whole word** and the text after it survives, so a reader
completing a name in the middle of a line keeps the argument they had typed.

## Measurement

```go
func DisplayWidth(s string) int
func CutColumn(s string, columns int) (string, int)
func CutColumnFromEnd(s string, columns int) string
```

`DisplayWidth` is in display columns and not bytes or runes, since a figure is
three bytes and one column. `CutColumn` returns the text and how many columns were
dropped, for a caller composing what fits beside something else.
`CutColumnFromEnd` keeps the tail, which is what a frame row wants: the field is
the thing a reader is looking for and the log beside it is what is cut.

## Diffs

```go
func DiffRows(reply string) ([]string, []Span)
func DiffSpans(rows []string) []Role
func IsDiffFence(info string) bool
```

A reply is split into rows and the rows that differ are coloured by role, which is
how an edit reads as an edit rather than as a wall of text. `IsDiffFence` tells a
fence that marks a diff from one that does not, since a model writes both.

## The exported surface

Verified against `go doc` on this tree rather than written from memory.

**Construction and lifecycle**

```go
func New(opts Options) *Session
func Start(ctx, s, screen, run, ask) error
func Run(s *Session, screen *Screen) error
func NewScreen(out io.Writer, size WindowSize) *Screen
func SizeOf(fd uintptr) WindowSize
func StdoutIsATerminal() bool
func StreamsAreTerminal(in io.Reader, out io.Writer) bool
func IsTerminal(fd uintptr) bool
func ReaderFor(in io.Reader) *bufio.Reader
```

`Run` draws once with no input, and is kept because the drawing is worth having on
its own: a test wants the bytes, and a redirected run has no terminal to read keys
from. `Start` is what a reader reaches.

**The terminal**

```go
type WindowSize struct { Rows, Cols int }
func (s *Screen) Size() WindowSize
func (s *Screen) SetSize(size WindowSize)
func (s *Screen) Height() int
func (s *Screen) Width() int
func (s *Screen) Write(text string)
func (s *Screen) Writer() io.Writer
```

`Write` is the only method that writes, so the lock serialising draws lives there
rather than in each caller. Two writers interleaving produce a row with half a
sequence in it, which is a colour bleeding into the next row. A screen with a nil
writer writes nothing rather than panicking.

**The session**

```go
func (s *Session) Log() *Log
func (s *Session) Options() Options
func (s *Session) SetModel(model string) error
func (s *Session) State() (State, string)
func (s *Session) SetState(state State, detail string)
func (s *Session) Ready() error
func (s *Session) Begin(ctx, question string, level int) (context.Context, error)
func (s *Session) Deliver(text string, level int)
func (s *Session) Notice(text string, level int, role Role)
func (s *Session) Finished(reason string)
```

`SetModel` is what a `/model` choice calls, so the file and the session cannot
disagree about which model is in force. It refuses an empty model rather than
storing one.

**The log**

```go
const LogBound = 20000
func (l *Log) Append(row Row) int
func (l *Log) Rows() []Row
func (l *Log) Len() int
func (l *Log) Folded() int
func (l *Log) Truncate()
func PlainRow(row Row) Row
func RowText(p Palette, row Row, width int) string
func WriteLines(w io.Writer, rows []Row) error
```

**The levels and workers**

```go
func (s *Session) OpenLevel(parent int, title string) (Level, error)
func (s *Session) CloseLevel(n int) error
func (s *Session) Level(n int) (Level, bool)
func (s *Session) Levels() []Level
func (s *Session) OpenLevels() []Level
func (s *Session) RowsAt(level int) []Row
func (s *Session) CopyLevel(n int) ([]Row, error)
func (s *Session) CopyText(n int) (string, Level, error)

func (s *Session) Spawn(parent int, question string) (*Worker, error)
func (s *Session) SpawnWithCancel(parent, question string, cancel func()) (*Worker, error)
func (s *Session) Workers() []*Worker
func (s *Session) WorkerAt(level int) (*Worker, bool)
func (s *Session) WorkerStateOf(w *Worker) WorkerState
func (s *Session) Finish(w *Worker, state WorkerState, reason string)
func (s *Session) Stop(w *Worker, reason string)
```

**The command table**

```go
func Lookup(name string) (*Command, bool)
func Names() []string
func Complete(prefix string) (names []string, whole bool)
func Completion(prefix string) (text string, caret int, whole bool)
func IsCommand(line string) (name, args string, ok bool)
func AttributeCommand() Command
func CloudflareCommand() Command
func TestCommand() Command
```

**The configuration writers**, which are how a preference reaches the file.

```go
func WriteModel(path, model string) error
func WriteModelSwap(path, model string) error
func ReadModelPair(path string) (model, last string, err error)
func ModelIsOffered(catalogue []string, model string) error
```

`WriteModelSwap` sets `model` while recording the one being replaced under
`last_model`. The pair is one read and one write, so a reader whose terminal died
between two writes is not left with a file that disagrees with itself.
`ReadModelPair` is exported for the same reason a writer is: a caller swapping the
pair needs to know what it is swapping from and to.

**Colour**

```go
type Palette struct { /* ... */ }
func NewPalette(on bool, chosen Ground, theme *Theme) Palette
type Ground int    // GroundAuto, GroundDark, GroundLight
func ParseGround(s string) (Ground, error)
func GroundCandidates() []string
func ThemeNames(t Theme) string
type Theme struct { /* ... */ }
func NewTheme(name, foreground, background string) (Theme, error)
type RGB struct { /* ... */ }
func RGBFromHex(s string) (RGB, bool)
type RoleTable map[Role]RGB
func RoleTableFor(g Ground) RoleTable
func Roles() []Role
func RoleName(role Role) string
```

**Rows and drawing**

```go
func DrawLog(w *Screen, rows []Row, palette Palette)
func DrawStack(w *Screen, rows []barRow, palette Palette)
func DrawFooterRow(w *Screen, at int, rows []barRow, palette Palette)
func LogRows(height int) int
func SweepText(text string, step int) string
```

**The bars**, exported and reached for by a caller outside the module.
`RenderTop`, `RenderBottom`, `RenderProvider`, `RenderStatus`, `RenderBar`,
`Field`. A bar is never cut and never drops a field; `barWidth` is 1 and
`joinWithin` joins the whole list at every width.

**The errors**

```go
ErrNoSize        the terminal reported no size
ErrNoTerminal    stdin and stdout must both be terminals
ErrNoModel       no model is chosen, so there is nothing to ask
ErrNoQuestion    no question was given
ErrNoLevel       there is no such level
ErrLevelClosed   that level has been closed
ErrRootLevel     that level is the conversation and cannot be closed
ErrCognito       a worker acts on the host, and this session records nothing
ErrNoWorker      nothing is running
ErrNoRawMode     the terminal would not enter raw mode
ErrNotReadable   this descriptor cannot be read without blocking
ErrNoColor       colour is off, so there is nothing to write
ErrClosing       the session is closing
```

**The constants**

```go
const Prompt        = "root@localhost $ "
const StatusFields  = 20
const FieldIndent   = 0
const LogBound      = 20000
const TermiosSupported = true
```

`TermiosSupported` is false on a platform with no spelling for the termios ioctl,
and is what a caller checks before trusting `IsTerminal`.

## Known gaps

1. **`DrawStack` and `DrawFooterRow` are exported over an unexported type.** A
   caller outside the module can call the function and cannot construct its
   argument. This is a defect rather than a decision.

2. **Thirty-five of thirty-nine commands have no body.** The table lists them, the
   dispatcher reports them as listed-but-not-run, and no handler exists.

3. **No command renders help.** The table carries summaries and nothing reads them.

4. **`Options.Verbosity` does not exist.** The frame carries a letter `v` in
   `fieldVerbosity` and nothing reaches the wire, so `/verbosity` has no body and
   `internal/verbosity` is not imported.

5. **A turn carries no history.** `ask` sends one user message and no
   conversation, so a follow-up question is blind to what came before it.

6. **`ModelIsOffered` has no caller.** It is the check a reader naming a model the
   endpoint does not offer would want, and it needs a catalogue that nothing
   fetches, since the transport has no models endpoint.