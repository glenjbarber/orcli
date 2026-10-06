# Interface documentation

Reference documentation for `orcli`, written from the source of this tree rather
than from a README. Every claim about behaviour names the function that carries
it, and every claim about intent is taken from the comment on that function.

These files describe `main`. Where an earlier build's documentation exists and
disagrees, this tree supersedes it. The decisions behind the current shape are
recorded one file each in `staged/`, listed in `staged/adr-index.txt`, and the
documentation this tree replaced is accounted for in
`staged/design-doc-superseded.txt` with the two overwritten files kept in
`doc-backup/`.

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

## What changed in the merge that brought this tree up to date

Four decisions, each recorded in full as a file in `staged/`. The short of them,
so a reader who knew the tree before does not have to find it.

**There is no `/connect`.** A reader's own first question is the probe. A turn
that came back with text has proved the credential and the model together, and
that is what writes `model` to the configuration file. A turn that delivered
nothing confirms nothing. See 0000003.

**`/model NAME` records the model it replaced** under `last_model`, and
`/model last` swaps the pair. Two of them put the members back where they were.
A choice moves the session as well as the file, so the frame does not draw one
model while the reader is answered by another. See 0000004.

**The frame owns every row** and there is no scroll region, so the log rides
beside twenty one-character status fields rather than occupying a region above
six footer rows. See 0000001.

**A bar never drops a field**, so the bar renderers join the whole list at every
width and a bar carries everything it has. See 0000002.

## What a worker needs to know before changing anything here

Four rules hold across the tree and a change that breaks one is a bug rather than
a style question.

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
| a retired level keeps what it was | `levels.go`, `CopyText` |
| a worker is refused under cognito | `ErrCognito`, `worker.go` |
| a confirmed turn writes the model, and only once | `TestAConfirmedModelIsWritten`, `TestTheModelIsWrittenOnce` |
| nothing is written before a turn is answered | `TestNothingIsWrittenWithoutAConfirmedTurn` |
| two `/model last` leave the file as it was | `TestLastTwiceIsANoOp` |
| a choice keeps the credential, the key order, and a single-line file | `TestTheWriteKeepsTheCredentialAndTheKeyOrder`, `TestTheWriteKeepsASingleLineFileOnOneLine` |

## The known gaps, so absence reads as a gap

Six things a reader looking for them will not find here.

1. **No scroll region exists.** The program owns every row, so there is nothing
   for a region to hold in place. What it cost is recorded in 0000001.

2. **The reader's own scrollback is not the transcript.** The transcript is drawn
   from rows the program holds, in the second column beside the status fields.
   What was on the screen before the program started is still there and is not
   rewritten, but a reader scrolling past the program's first row finds their
   shell rather than the conversation.

3. **A figure field is one digit.** A field is one character, so `count` in
   `internal/tui/run.go` truncates a count to its first digit. A log holding
   nineteen thousand rows reports `1`. The full number is in the transcript, and
   the field names say which they are.

4. **A turn carries no history.** `ask` sends one user message and no
   conversation, so a follow-up question is blind to what came before it.

5. **`internal/verbosity` is untracked and unimported.** The six-level ladder is
   the specification, and nothing reaches the wire. The frame carries a letter `v`
   and no level is asked for.

6. **`DrawStack` and `DrawFooterRow` are exported over an unexported type.** A
   caller outside the module can call them and cannot construct the argument.

## The reference documentation this tree supersedes

Four of the six files the reader added under `doc/` are still there, untracked,
and describe a build this tree is not:

```text
interface-bell.md          the bell and the preference behind it
interface-connect.md       a connection command, which this tree does not have
interface-permission.md   the approval rules
interface-verbosity.md     seven levels, where this tree has six
```

The two that could not both exist at the same path were overwritten by the merge
and are kept in `doc-backup/`. None of them is reconciled, since this tree
supersedes what they describe and a reader wanting the older build's account has
them.
## Dated feature implementation evidence

[Session ownership implementation evidence](session-ownership-implementation.md)
records the local phase 2 ownership changes on `feature/tui-redesign` and their
pending delivery. It is separate from the main-branch interface reference above.
