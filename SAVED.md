# SAVED.md

State of the `tui-redesign` project. It is a record of what was asked and
what is open, not a design. The design is in `DESIGN.md` and the API is in
`staged/adr-0000010-interface-api.txt` beside `staged/adr-0000011-*.txt`.

It lives at the root of the worktree, is untracked, and is not ignored.

**This file is the superseding design for everything mentioned in the past
streams.** Where an earlier record and this one disagree, this one is the
design.

## The next thing to do

**Copy this file.** It is the state of the project and it is untracked, so a
checkout loses it. Copy it somewhere outside the repository before anything
else is done, and the next session starts by reading the copy.

`give2spawn.txt` at the root of this worktree is what to hand a worker to start
on the core pieces. It is copy and paste friendly.

## What was asked

Project specs for a TUI built with an external library. The requirements are
copy, paste, a scrollback buffer, resizing, and panes. The workflow is to act
like a terminal multiplexer: the main pane is `tty0`, pane 1 is `tty1`, the
main pane is 0, and `Ctrl+B` is the prefix.

## The screen, as settled

Top to bottom, seven regions. The order is the reader's, given as a correction
to an earlier draft.

```
live window
scrollback indicator
mouse indicator
status bar 1   Provider | Model | Status | Bell | Verbosity
status bar 2   Hostname | Context | Autosave | Stealth | Approval
prompt         root@lolhost @ <input>
pane bar       0 | 1 | 2
```

- The two status bars do not swap. An earlier draft reversed them.
- Approval is carried once, in the second bar. A draft named it in both and
  said "it is not twice, it should go here".
- Verbosity falls off when bar 1 is too narrow.
- Status carries colour when the pane is active.
- The pane bar is below the prompt. An earlier draft had pane numbers first.
- The prompt is `root@lolhost @ `, written deliberately and marked as not a
  typo. It replaces `root@localhost $ `.

## Panes, as settled

- Pane 0 is the main session and cannot be closed.
- A new pane takes the lowest available numeric ID, so with 0, 1 and 2 open,
  closing 1 and opening a pane gives 1.
- Panes do not renumber. Closing pane 2 of five leaves 0, 1, 3 and 4.
- `/pane N` switches to pane N. `/pane next` and `/pane previous` step
  through them, wrapping at both ends.
- `/close` closes the focused pane, and the reader gets back to the interface
  straight after a paste.
- A pane ID is not a level. `internal/tui/levels.go` retires a level rather
  than returning it to a pool, since a reader who copied level 3 an hour ago
  must not get whatever took the number. Pane IDs are reused and levels are
  not, and that is two guarantees for two readers rather than a contradiction.

## Permissions, as settled

- `/permit`, `/deny`, `/approve` and `/reject` are per-pane.
- `/permit [dir]` allows read to a directory. `/deny [dir]` does the opposite.
- The directory list is saved in a SQLite database rather than in the
  configuration file.
- The reader is reminded of the pane's permissions and the path to escalating
  at startup.

## Restore, as settled

- `/restore` loads the last-known session.
- The latest session for an identifier is a symlink named
  `%SOME_IDENTIFIER%.latest`, pointing at the session file it names, so a
  restore is not a search for the newest file and does not depend on the clock.
- Context has to survive a restart for a restore to be worth anything. What
  context is carried is open, and it is why the reader asked.

## The twiddle, as settled

A three by two block of single-dot Braille characters, six cells, three
columns. The colour cycles as the model works: on a dark terminal, yellow,
cyan, bright blue; on a light terminal, the opposite palette. The colours are
xterm256-compatible and fall back to the sixteen ANSI colours, and bright
blue is a named ANSI colour in the sixteen.

This replaces the two-cell lemniscate the tree draws and the figure-eight in
the reader's earlier notes.

## The thinking word, as settled

The word beside the twiddle alternates through developer-friendly phrases on
a timer. One is `No Segmentation Fault (no core dumped)`. The timer resets on
every new update from the OpenRouter API, or every seven seconds, whichever is
shorter. `thinking` is one of the phrases rather than the only one. The rest
of the set is open.

## The input field, as settled

- `Ctrl+A` moves the caret to the start of the line.
- `Ctrl+W` erases the word before the caret.
- `Ctrl+U` erases from the caret to the start of the line.
- Up and Down walk the input history, one entry per press, stopping at each end.
- Deduplicated: an entry that duplicates an earlier one is kept once, at the
  position of its most recent occurrence, and the history is ordered most
  recent first.
- Every line that was sent is remembered, whatever it was. A queued line is
  remembered when it was sent rather than when it was queued.
- Walking the history does not send anything and does not clear the line.

## The queue, as settled

- A message typed while the model is working is queued, first in first out.
- Escape sends the head of the queue now, bypassing the wait.
- A message sent by escape leaves the queue when the model has processed it,
  not when it is sent.
- When the turn settles, each queued message is sent one per turn.
- Where the queue lives is open. SQLite was raised for it and would make
  pending work survive a crash and make a daemonizing case honest.

## The live window contents, as settled

- Output errors are shown in the live window, beside the tool call or reply
  they came from.
- Input errors are shown in the input field and clear on a timeout.
- Tool output such as `[fs] read_file [...]` is off by default and
  `/verbose` turns it on.
- A tool line carries an icon where the terminal can draw one, and a shell
  command is a terminal icon.
- Rows arriving from a running turn are held, not discarded. The live activity
  readout is suppressed while the reader is scrolled away, `[scrollback]` is
  shown, and both end at the live edge.

## Copy and paste, as settled

`/copy` copies the entire reply, `/copy N` copies N replies back, and
`/copy N M` copies fenced block M of reply N. `/paste` sends the last copy to
the model, so a reader scrolls back, finds a numbered block, copies it and
pastes exactly that block. `/paste` with nothing copied is refused by name.

## Commit attribution, as settled

A model should use `git config get` to determine its commit attribution, and
that attribution is stored in `.orcli.json`. The member name is open.

## The audience

sysadmin, business builder, marketing, finance, and productivity, all in one.
`/cloudflare` is the named example of a command that should be manageable
through an in-terminal menu. A session log is retained in case the
application crashes.

## The shell line rule, as settled in intent

Input that looks like a shell command is a no-op when it is alone in a string,
so `ls` and enter in the wrong terminal window does nothing. It is not a no-op
when it is directly in response to a question from the model. This belongs to
the introductory setup with the model underneath.

What makes a line a shell line, and what "directly in response to a question"
means, are both open.

## Working in this repository

`AGENTS.md` is read by default and holds the operating rules. Every reference
to reading `.orcli.md` in the source is retired. `AGENTS.md` has been rewritten
so that it no longer points at another file for its rules, and it now carries
the command rule, the writing rules, the interaction rules and the backlog
limit that were kept elsewhere.

Two references remain and are deliberate. `.subagent.md` is read-only and still
names `.orcli.md`, and `staged/design-wiring.txt` is a committed record of a
unit worked in the past that describes where its design was put at the time.
Neither is an instruction to read it now.

## What was written

- `staged/adr-0000006-queue-one-per-turn-and-escape-sends.txt`.
- `staged/adr-0000007-the-interface-is-a-multiplexer.txt`.
- `staged/adr-0000008-live-view-status-pane-view.txt`.
- `staged/adr-0000009-live-view-contents-and-the-prompt.txt`.
- `staged/adr-0000010-interface-api.txt`. The API document.
- `staged/adr-0000011-editing-keys-and-input-history.txt`. The editing keys and
  the history.
- `staged/adr-index.txt`. Extended from five records to eleven.
- `DESIGN.md`. The design of record, to be published on GitHub.
- `SAVED.md`. This file.
- `AGENTS.md`. Rewritten to hold the operating rules itself.
- `give2spawn.txt`. What to hand a worker to start on the core pieces.

## What is superseded

0000010 supersedes 0000008 and 0000009, and the pane arrangement of 0000007. Both stand
as records of a decision that was taken and then replaced.

0000011 fills the gap 0000010 left in the keys a reader touches, and supersedes
nothing.

What survives from 0000007: a session per pane, pane 0 as the main session,
`Ctrl+B` as a non-latching prefix, program-owned scrollback, and a session log
that survives a crash.

0000006 stands whole. 0000003 and 0000004 describe behaviour in the tree and are
unaffected. 0000001 and 0000002 describe the frame that is being replaced, and 0000010
replaces both.

No library named `tvlist` appears in any file in this repository. The only
third-party interface library named in any record is `tview`.

## What is not decided

- Which region yields first when the terminal is short, and how many rows each
  takes.
- What rings the pane bell.
- Whether the pane bar scrolls or folds.
- The `Ctrl+B` command set, and whether the prefix is escapable.
- Whether the prompt belongs to the focused session.
- The clipboard path on macOS Terminal.app with Secure Keyboard Entry, and
  whether copy is owned by the session or by the terminal.
- Whether `/copy last` exists, and whether a fenced block is numbered per reply
  or per session.
- Where the message queue lives, and whether a queued message is durable
  before it is sent.
- What `%SOME_IDENTIFIER%` is.
- The set of thinking phrases beyond the one given.
- How long an input error stays before it clears.
- Which tools get an icon, and what the icon is when the terminal cannot draw
  one.
- The permissions database schema.
- The member name for the commit attribution in `.orcli.json`.
- How many history entries are kept, whether a long one is shortened, whether
  the history is saved with the session, and whether a bare command is
  remembered.
- What makes a shell line a no-op, and what "directly in response to a
  question" means.
- The business scope from the roadmap. Outside this unit.

## Findings from the tree

Read while saving state. None of these is a design decision.

**Levels are the identity a row carries, not a pane identity.**
`internal/tui/levels.go` retires a level rather than returning it to a pool.
Level 0 always exists and cannot be closed. `Session.CopyLevel(n)` separates an
absent level from a closed one with `ErrNoLevel` and `ErrLevelClosed`.

**`/copy N M` has no meaning yet.** The command table names `copy` with the
summary "copy the level N, or the whole conversation", so `N` currently means
a level. The reader gave `N` a different meaning, a count of replies back.
That is a supersession and it is written into 0000010.

**The queue in the tree is not built.** `/queue` is in the command table with
no handler.

**`internal/tui` has no `SetScrollRegion`.** The frame writes terminal rows
directly. The old scratch renderer under `staged/frame-render/` may use the
old frame API and can make a broad build fail.

**The command table is large.** It names help, version, key, search, models,
freemodels, model, new, bell, color, cognito, verbosity, verbose, delegate,
pane, spawn, btw, close, queue, redirect, main, compact, save, load, mouse,
pause, clear, info, copy, permission, autosave, approve, tools, quit, exit and
test. Several have no handler.

## Gates

Run in this worktree, for the documentation change, over `./internal/...` and
`./cmd/...`:

```text
go build   clean
go vet     clean
gofmt -l   clean
go test    eight packages, all pass
```

No code has been written for the new interface.