# The frame

Twenty rows of one-character status fields in the first column, the log in the
second, and the prompt on the last row. The program owns every row of the
terminal and sets no scroll region.

Code: `internal/tui/stack.go` in full, `internal/tui/caret.go`, and the painter in
`internal/tui/run.go`.

The decision that replaced the previous frame is 0000001 in
`staged/adr-0000001-frame-owns-screen.txt`, and what it cost is at the end of this
file.

## The shape

```text
80x20 terminal, one field per row, the log beside it

 1  C   the oldest log row on screen
 2  S
 3  -
 4  -
 5  -
 6  S  STATE                  the state, carried as a word
 7  -
 ...
17  -  the oldest log row
18  -  a newer log row
19  -  the newest log row      a row arriving pushes everything above it up
20  root@localhost $ typed    the prompt, and the caret
```

The count is a ceiling rather than the terminal's height. `screenRows` returns
`StatusFields` on any terminal tall enough and the terminal's height below that,
so a 24 row terminal draws twenty and leaves rows 21 to 24 to the terminal.

```go
const StatusFields = 20
const statusCount = StatusFields
const statusGap   = " "
const promptField = ">"
```

`statusCount` is written as `StatusFields` rather than as the index of the prompt
plus one, and the comment on it records why: a field added above the prompt
without the count moving is a frame one row taller than the height the reader
asked for.

## The fields

Twenty named constants, in the order they read down the screen. The order is the
reader's: who the session is, what is running, what the turn is doing, what the
reader may act on, and what the session is holding.

| # | Field | Carries | Glyph |
| --- | --- | --- | --- |
| 0 | `fieldCwd` | the directory | `.` |
| 1 | `fieldSession` | which session | `1` |
| 2 | `fieldProvider` | the endpoint host | first character, `-` if none |
| 3 | `fieldModel` | the model | first character, `-` if none |
| 4 | `fieldKey` | whether a credential is set | `k` or `-` |
| 5 | `fieldState` | the state | the word, not a glyph |
| 6 | `fieldFigure` | the twiddle | `t`, `w`, or a space |
| 7 | `fieldApproval` | whether programs run unasked | first character, `-` if none |
| 8 | `fieldVerbosity` | how much is asked for | `v` |
| 9 | `fieldCognito` | whether nothing is recorded | `n` or `-` |
| 10 | `fieldColor` | colour on or off | `c` or `-` |
| 11 | `fieldMouse` | mouse reporting | `m` or `-` |
| 12 | `fieldCopy` | whether a copy is available | `y` or `-` |
| 13 | `fieldBell` | the bell | `b` or `-` |
| 14 | `fieldPane` | which pane | `0` |
| 15 | `fieldWorkers` | running workers | a count |
| 16 | `fieldQueue` | queued prompts | a count |
| 17 | `fieldHeld` | rows the log holds | a count |
| 18 | `fieldFolded` | rows the log dropped | a count |
| 19 | `fieldPrompt` | the reader's own line | `>` and the prompt |

**A field is one character.** That is the reader's decision and it is 0000002 in
`staged/adr-0000002-one-character-field.txt`. A field that cannot say what it means
in one character says nothing, so every glyph is a letter a reader can read rather
than a figure a reader has to learn, and `TestEveryFieldIsOneCharacter` holds
every field to one character except the two named below.

**Two fields are not one character.** `fieldState` is carried as the whole word,
since it is the field a reader watches second by second and a state spelled `idle`
is a state rather than an `i` a reader has to learn. The prompt row is `>` followed
by the prompt and whatever the reader typed, since a row cannot be a glyph and a
prompt at once.

**A count is one digit.** `count` in `run.go` truncates. A log holding nineteen
thousand rows reports `1`, and the field names say which count it is.

**`fieldModel` moves with `/model`.** A choice calls `Session.SetModel` as well
as writing the file, so the glyph beside the prompt is the model the next turn is
sent with rather than the one the file held when the frame was drawn. See 0000004 in
`staged/adr-0000004-model-choice-records-replaced.txt`.

## The log rides beside the fields

`stackLines` gathers the tail of the log newest-first and fills rows upward from
the prompt, so a row arriving appears just above the prompt and everything above it
moves up.

```go
shown := make([]Row, 0, logRows)
for i := len(log) - 1; i >= 0 && len(shown) < logRows; i-- {
    shown = append(shown, PlainRow(log[i]))
}
```

The rows are filtered through `PlainRow` on the way, which is what keeps a model
from steering the terminal through its own reply.

A log longer than the frame fills upward and the oldest rows are off the screen.
The number of rows visible is `LogRows(height)`, which is `screenRows(height) - 1`.

## Drawing

`DrawStack` addresses every row by position and clears it before writing:

```go
for i, row := range rows {
    w.Write(escapePosition(footerScreenRow(w, i), 1))
    w.Write(escapeEraseLine)
    w.Write(frameLine(row, w.Width(), palette))
}
```

It is written by position rather than from wherever the cursor is, since the frame
owns the screen and does not inherit the cursor from the shell that ran before it.
Each row is cleared rather than overwritten, since a row being written is shorter
than the row it replaces and writing over the top leaves the tail of the old row
visible.

`footerScreenRow(screen, k)` is `1 + k`. There is no arithmetic against the
terminal's height, and the comment on it records why: a row named by a height is a
row that moves when the height is read a second time.

**The log beside a field is cut from the tail.** `frameLine` keeps
`width - fieldWidth - gapWidth` columns and calls `CutColumnFromEnd`, so the field
is never the thing cut. Cutting the field would move every log row one column left,
and the field is the thing a reader is looking down the column for.

## The three cases in paint

```go
switch {
case !l.footerDrawn:
    DrawStack(screen, frame, palette)
case sameFooterExceptField(l.footer, footer) && !grown:
    DrawFooterRow(screen, len(frame)-1, frame, palette)
default:
    DrawStack(screen, frame, palette)
}
```

A keystroke changes only the prompt row, so only that row is written and nothing
moves. A log row or a field change rewrites every row, since the log rows beside
the fields moved and a row left alone is a row showing a log row that is no longer
there.

`grown` is `len(rows) != l.drawn`, and it is needed because a log row arriving
while the fields are unchanged still has to repaint: `sameFooterExceptField` only
compares the fields.

## The caret

`placeCaret` in `caret.go` writes the row and column as absolute values and then
advances forward from column one:

```go
column := FieldIndent + DisplayWidth(Prompt) + DisplayWidth(before)
l.screen.Write(escapePosition(promptScreenRow(l.screen), 1))
l.screen.Write("\r")
l.screen.Write(fmt.Sprintf("\x1b[%dC", column))
```

Three decisions in that.

**The row is asked for, not counted.** `promptScreenRow` is the last row the frame
draws, so a caret is where a reader expects to find it without being told the
frame's height.

**The column is in display columns, not runes or bytes.** A field holding a wide
character is two columns per character and a byte count lands the caret inside a
glyph. The text before the caret is measured rather than the caret's index.

**It never moves backwards.** There is no sequence in `caret.go` that moves the
cursor up through the frame. `TestTheCaretIsNotMovedBackwards` holds it.

A caret past the right edge is placed at the last column, since a terminal clamps
a position past the end to its own edge and that reads as a caret the reader cannot
account for.

## The bars that remain

`RenderTop`, `RenderBottom` and `RenderBar` are still exported and still join
their whole field list. They are what the command table's documentation refers to
and what a caller outside the module reaches for.

`barWidth` is 1, which is a floor and not a cap. `joinWithin` joins the whole
list at every width and never drops a field, so a bar carries everything it has
and overflows its row rather than losing the end of itself.
`TestABarKeepsEveryFieldAtEveryWidth` holds it at widths 1 through 200.

## What this costs, recorded

The reader's own scrollback is not the transcript. It is whatever the terminal kept
from before the program started. The transcript is drawn from rows the program
holds, in the second column.

That is the trade 0000001 weighs, and `staged/design-status-bar-ui.txt` sets out the
three arrangements considered and why this one was chosen. The alternate screen is
where the bars could be redrawn per paint, and it was not taken.

## Known gaps

1. **The log is not scrollable.** The frame draws the tail of the log and there is
   no way to ask for the rows above it. `Log.Folded` reports how many were dropped,
   but a reader who wants to see row 4,000 of a 5,000 row log has no route to it.

2. **The cwd field is a dot.** `fieldCwd` is filled with `.` rather than the
   working directory, and `opts.WorkingDir` is available to render it. Nothing
   fills it because a path is wider than one character and the field is not.

3. **Pane navigation is limited to two sessions.** The pane bar follows the
   focused session: pane 0 is the main session, and pane 1 is the latest `/begin`
   session. A full pane set is not built.

4. **`fieldVerbosity` is a letter.** It reads `v` and nothing reaches the wire,
   since `internal/verbosity` is not imported.

5. **The frame is unverified on a real terminal.** Every test draws to a buffer,
   and whether twenty rows and a caret on row twenty look right needs a screen.
