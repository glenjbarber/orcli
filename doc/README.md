# Interface documentation

Reference documentation for `orcli`, written from the source of this branch rather
than from a README. Every claim about behaviour names the function that carries it,
and every claim about intent is taken from the comment on that function.

These files describe the build on `feature/bottom-console-input`. Where an earlier
build's documentation exists and disagrees, this branch supersedes it; see
`staged/design-doc-superseded.txt` for the differences and the reasoning.

## The files

| File | Covers |
| --- | --- |
| [interface-startup.md](interface-startup.md) | the flags, the trust gate, the configuration read, and the order startup runs in |
| [interface-frame.md](interface-frame.md) | the twenty-row frame, the one-character status fields, the log riding beside them, and the caret |
| [interface-session.md](interface-session.md) | the log, the levels, the workers, the states, and the turns |
| [interface-commands.md](interface-commands.md) | the command table, the dispatcher, completion, the line editor, and the exported surface |

## Reading order for a worker

`interface-startup.md` first. It establishes that the credential is read from a
file and from nowhere else, that the interface is opened only between two
conditions, and that the whole of startup is a function whose streams are
parameters.

`interface-frame.md` second, because the frame is what every other part of the
interface is drawn into and a change anywhere else shows up there first.

`interface-session.md` third. The log, the levels and the workers are what a
command manipulates and what `/copy N` reaches.

`interface-commands.md` last, since it is the table and the exported surface, and
it assumes the other three.

## What a worker needs to know before changing anything here

Four rules hold across the tree and a change that breaks one is a bug rather than a
style question.

**A row leaves as plain text.** `plainRow` in `internal/tui/log.go` strips the
C0 and C1 controls and the delete character, keeping only the tab. A model can
write anything into a reply, so a row that is not filtered before it is drawn is a
model steering the terminal.

**The credential never reaches a row, a diagnostic, or the environment.** It is
read from the configuration file and from nowhere else. `OPENROUTER_API_KEY` is
ignored even when set, since a file shadowed by a stale value elsewhere is a
failure a reader cannot see.

**A path outside the working directory is refused** before anything is opened or
run, and containment for a subprocess is an open descriptor rather than a prefix
comparison, since a cleaned path comparison is defeated by the symlink it cannot
see.

**A call always produces a result.** An unknown name, a malformed body, and a
panic in a request are all reported. A call producing nothing leaves a turn
waiting for something that never arrives.

## The invariants that tests hold

Each of these is asserted by a named test, and changing the behaviour means
changing the test and saying why.

| Invariant | Where it lives |
| --- | --- |
| the frame is twenty rows and each carries one field | `TestTheFrameIsTwentyRows` |
| a field is one character, except the state and the prompt | `TestEveryFieldIsOneCharacter` |
| the newest log row sits beside the prompt | `TestTheLogIsNewestBesideThePrompt` |
| the caret never moves backwards through the frame | `TestTheCaretIsNotMovedBackwards` |
| a bar never drops a field to fit a width | `TestABarKeepsEveryFieldAtEveryWidth` |
| a multi-line result becomes one row per line | `TestSplitRowsMakesOneRowPerLine` |
| a row carries no escape | `TestAnEscapeDoesNotReachTheTerminal` |
| the log never grows past `LogBound` and says how many it dropped | `Log.Folded` |
| a retired level keeps what it was | `TestTheCaretIsNotMovedBackwards` and `levels.go` |
| a worker is refused under cognito | `ErrCognito`, `worker.go` |

## The known gaps, so absence reads as a gap

Three things a reader looking for them will not find here.

1. **No scroll region exists.** The program owns every row, so there is nothing
   for a region to hold in place. The bug that region caused is recorded in
   `staged/design-status-bar-ui.txt`: it took a row count and wrote its first row
   as one, which put the rows the reader was typing inside it.

2. **The reader's own scrollback is not the transcript.** The transcript is drawn
   from rows the program holds, in the second column beside the status fields.
   What was on the screen before the program started is still there and is not
   rewritten, but a reader scrolling past the program's first row finds their
   shell rather than the conversation.

3. **A figure field is one digit.** A field is one character, so `count` in
   `internal/tui/run.go` truncates a count to its first digit. A log holding
   nineteen thousand rows reports `1`. The full number is in the transcript, and
   the field names say which they are.