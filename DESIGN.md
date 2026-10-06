# Design

This document is the design of record for the interface being built. It
describes what the program is and how its parts fit. Where it disagrees with
`AGENTS.md`, one of the two is wrong and the disagreement is a bug to be fixed
by name in the commit that fixes it.

**Nothing in this document is in the tree yet.** There is no pane, no prefix
key, no viewport, no queue and no restore. What follows is a design, and the
sections marked open are decisions that have not been taken.

The decisions behind it are the records under `staged/`. 0000001 through 0000005
describe the frame that is being replaced and two behaviours in the tree that
stand. 0000006 through 0000011 are the interface being designed, and 0000010 is the
API document with 0000011 beside it.

**`AGENTS.md` is read by default,** and no file in the source directs a reader
to `.orcli.md` any more.

`AGENTS.md` records what to do and where things are. This records what the
system is meant to be. Neither records why a decision was taken.

## 1. What the program is

`orcli` is a terminal client for the OpenRouter API. It is an interface for
holding several AI conversations at once, reading one while another is working,
moving between them without losing any, and putting back what was there before.

It is a multiplexer in the sense the reader means. The main pane is `tty0`,
pane 1 is `tty1`, and a reader who knows `tmux` knows how to move between
them. What it is not is `tmux`: the panes are listed in a bar at the bottom
rather than tiled, and the screen is a column of regions rather than a grid.

The reader is the audience. A sysadmin, a business builder, someone in
marketing, someone in finance, and someone who wants to be productive, all
using the same program. That is recorded because it has a consequence, and the
consequence is that a capability a specialist would configure has to be
reachable by someone who will never read a file.

## 2. The screen

Seven regions, top to bottom. The order is the reader's.

```
live window
scrollback indicator
mouse indicator

status bar 1   Provider | Model | Status | Bell | Verbosity
status bar 2   Hostname | Credits | Cost | Context | In | Out | Autosave | Stealth | Approval

root@lolhost @ <input>

pane bar       0 | 1 | 2
```

**The two status bars do not swap.** An earlier draft had them reversed and the
reader wrote "hold the swapping".

**Approval is carried once, in the second bar.** A draft named it in both and
then said "it is not twice, it should go here".

**Verbosity falls off when bar 1 is too narrow.** It is the last field and the
first to go, since the bar is about the model answering and not about how much
of it.

**Status carries colour when the pane is active,** so a reader reading the
pane bar can see which pane is focused without switching to it.

**The pane bar is below the prompt.** An earlier draft had pane numbers as the
first bar and the reader inverted it.

Which region yields first when the terminal is short is open. The live window
should yield last, since a reader reading history is using it.

## 3. Panes and sessions

**A session is a conversation.** It owns the messages, the transcript, the
cursor, where the reader has scrolled to, what `/copy` copies, what `/permit`
allows, and the input history. It owns none of the screen.

**A pane is an entry in the pane bar.** It points at one session and is reached
by naming it.

**Pane 0 is the main session.** It exists at startup, it holds the
conversation the reader is in unless they leave it, and it cannot be closed.

**A new pane takes the lowest available numeric ID.** With 0, 1 and 2 open,
closing 1 and opening a pane gives 1, not 3.

**Panes do not renumber.** Closing pane 2 of five leaves 0, 1, 3 and 4. A
number means the pane that was given it, so a reader who learned what pane 3
is does not find something else there later.

**A pane ID is not a level.** `internal/tui/levels.go` retires a level rather
than returning it to a pool, and gives the reason: a reader who copied level 3
an hour ago and types `/copy 3` now would get whatever took the number. Pane
IDs are reused and levels are not, and that is two guarantees for two different
readers rather than a contradiction. A pane ID is how a reader navigates. A
level is how a row is attributed.

**`/pane N` switches to pane N, and `/pane next` and `/pane previous` step
through them,** wrapping at both ends.

**The prefix is `Ctrl+B`,** because it is the `tmux` prefix and the reader
named it. It is non-latching: the reader presses and releases it, and the next
key is a command rather than text. Without the prefix, every key belongs to
the focused session.

The command set behind the prefix is open. Nothing has been designed.

**`/close` closes the focused pane,** and the reader gets back to the interface
straight after a paste. Closing the main pane is refused.

**The set holds which pane is focused,** and the keys and the commands read
that one place.

## 4. The prompt

`root@lolhost @ ` and a space before the caret.

It is not computed. It is not the reader's user, their host or their
directory, and a reader on a machine whose host is `lolhost` still gets
`lolhost`. The reader wrote `root@localhost $ ` first and then this, and marked
the second as deliberate rather than a typo.

The prompt belongs to the focused session, which follows from a pane being a
viewport. It is not ratified and is open.

## 5. Scrolling and scrollback

**The program owns the scrollback.** The terminal's buffer is shared by
everything on the screen, so a pane that used it would put every pane's history
into a place no pane owns.

**Scrolling is read, not stored.** The live window says where its viewport is
relative to its bottom, so being at the live edge is a fact rather than a mode
the program keeps in step. There is no separate reading-mode variable.

**Rows arriving from a running turn are held, not discarded,** in a queue in
the session, folded into the live window when the reader returns to the live
edge. Rows are folded at draw time and never on arrival.

**The live activity readout is suppressed while the reader is scrolled away.**
Progress reporting is a reading of the present, and a reader reading history is
not present. `[scrollback]` is shown while it is suppressed, and both end when
the viewport reaches the live edge. Transcript rows are deferred and kept; live
activity is not shown. Those are two different things.

**The mouse indicator is its own region** and says whether the wheel is being
read, since a reader whose wheel stopped needs to know whether the program
stopped reading it or the terminal stopped sending it.

**A terminal with no wheel must still be scrollable,** since there is no
scrollback to scroll.

## 6. The live window

**The live window is where the model writes.** The question, the tool calls,
the answers, the reply, and the errors the model produced.

**Output errors are shown,** beside the tool call or reply they came from. A
reader who cannot see an error will believe the tool worked.

**Input errors are shown in the input field and clear on a timeout.** They do
not become rows, since a row is the record of what happened and an input error
never became a question. How long they stay is open.

**Tool output is off by default and `/verbose` turns it on.** A line such as
`[fs] read_file [...]` is the shape of it.

**A tool line carries an icon where the terminal can draw one.** A shell
command is a terminal icon. Which tools get one is open.

## 7. The twiddle

**A three by two block of single-dot Braille characters,** six cells, three
columns.

**The colour cycles as the model works.** On a dark terminal the cycle is
yellow, cyan, bright blue. On a light terminal the opposite palette. The
colours are xterm256-compatible and fall back to the sixteen ANSI colours, and
bright blue is a named ANSI colour in the sixteen, so the cycle works on a
terminal with eight colours.

## 8. The thinking word

**The word beside the twiddle alternates through developer-friendly phrases.**
One is `No Segmentation Fault (no core dumped)`.

**The timer resets on every new update from the OpenRouter API, or every seven
seconds, whichever is shorter.** A fast stream changes the phrase often and a
slow one still changes it.

`thinking` is one of the phrases rather than the only one, since a reader who
has seen it for two minutes learns nothing from it. The rest of the set is
open.

## 9. Messages while a model is working

**A message typed while the model is working is queued,** first in first out,
one queue per session.

**Escape sends the head of the queue now,** as an update to the request in
flight, so the tool calls already made and answered stay answered.

**A message sent by escape leaves the queue when the model has processed it,**
not when it is sent, so a reader who presses escape twice can tell whether the
first press landed.

**When the turn settles, queued messages are sent one per turn.** Three
questions in one request cannot be attributed in the transcript, and the queue
exists so a reader can see which answer answers which question.

**Where the queue lives is open.** SQLite was raised for it and would make
pending work survive a crash and make a daemonizing case honest.

## 10. Copy, paste and restore

`/copy` with no argument copies the entire reply. `/copy N` copies N replies
back. `/copy N M` copies fenced block M of reply N.

This differs from the command table in the tree, where `/copy N` names a
level, and the difference is a supersession: the reader gave `N` a meaning and
it is a count.

**`/paste` sends the last copy to the model.** The reader scrolls the live
window, copies a numbered fenced block, and pastes exactly that block back. It
is refused by name when nothing has been copied.

**`/restore` loads the last-known session.** The latest session for an
identifier is a symlink named `%SOME_IDENTIFIER%.latest` pointing at the
session file it names, so restoring is not a search for the newest file and
does not depend on the clock. What `%SOME_IDENTIFIER%` is remains open.

**Context has to survive a restart for a restore to be worth anything.** A
session restored with its transcript but not its context is a transcript the
reader has to read rather than a conversation they resume. What context is
carried is open, and it is the reason the reader asked for this.

Whether `/copy last` exists, and whether a fenced block is numbered per reply
or per session, are open. Whether copy is owned by the session or by the
terminal is open.

## 11. The clipboard on macOS Terminal.app

The gate the reader set: all of this has to work in Terminal.app with the
default settings, and all of our own code for copy, paste and mouse scrolling
is to be turned off entirely.

`pbcopy` is ruled out. `pbpaste` is a possible macOS-only path, with the whole
path disabled when the host is not macOS and the access boundary decided
before it is exposed to the model as a tool.

A cell grid library has a clipboard write and a clipboard read, the read is
documented as something a terminal may ignore, and paste into the program is
not that read at all. Paste arrives as ordinary input bytes through the
terminal's own Edit menu.

Secure Keyboard Entry affects delivery of keystrokes and is separate from
clipboard permission. The exact copy path is unresolved.

## 12. Resizing

**A resize reflows, and the session does not change.** The regions own the
rectangles and the session owns the content.

**A resize never loses a row.** A reply folded at one width is refolded from
the whole reply, which is why the session holds the untruncated reply.

## 13. Permissions

**`/permit`, `/deny`, `/approve` and `/reject` are per-pane.** A pane opened
with no tools is a pane with no permission.

**`/permit [dir]` allows read to a directory. `/deny [dir]` does the opposite.**
Both store into a SQLite database rather than into the configuration file, so
a reader with several hundred directories can add and remove them without
rewriting a JSON document.

The database holds at least a pane, a path, a verdict and a moment. The schema
is not settled.

**The reader is reminded of the pane's permissions and the path to escalating
at startup.** That is step two of the workflow.

The tree has no per-path permissions. `internal/config` carries three
session-wide modes, `ask`, `allow` and `deny`, and its own comment says a mode
is set once for a session rather than per call. A pane permission that can be
escalated is not that.

## 14. Commit attribution

**A model should use `git config get` to determine its commit attribution, and
that attribution is stored in `.orcli.json`.** The value the program used is
written where a reader can see it. The member name is open.

## 15. The input field

The field holds one line. It is edited in place and it is the only place a
reader types.

**`Ctrl+A` moves the caret to the start of the line. `Ctrl+W` erases the word
before the caret. `Ctrl+U` erases from the caret to the start of the line.**
The emacs spellings, because that is how the reader named them. `Ctrl+A` is
not the prefix, which is `Ctrl+B`.

**Up and Down walk the input history**, one entry per press, stopping at each
end rather than wrapping.

**A history entry that duplicates an earlier one is kept once, at the position
of its most recent occurrence, and the history is ordered most recent first.**
Three `ls` presses over an hour give one `ls`, at the top.

**Every line that was sent is remembered**, whatever it was. A question, a
command and a pasted paragraph are all history.

**A line that was queued is remembered when it was sent**, not when it was
queued, so the history holds what the model was actually asked.

**Walking the history does not send anything and does not clear the line.** Up
replaces the field and leaves the caret at its end.

**Deduplication is on the exact text.** Two lines differing only in trailing
whitespace are two entries.

How many entries are kept, whether a very long one is shortened, whether the
history is saved with the session, and whether a bare command word is
remembered at all, are open.

## 16. Commands

`/pane N`, `/pane next`, `/pane previous`, `/close`, `/copy`, `/copy N`,
`/copy N M`, `/paste`, `/restore`, `/verbose`, `/permit`, `/deny`,
`/approve`, `/reject`, `/stealth`, `/model`, `/btw`.

`/cloudflare` is the named example of a command that should be manageable
through an in-terminal menu rather than by typing. A menu is a form of the
multiplexer rather than a departure from it: `tmux` shows a list for its
command prompt, and it is the only form that reaches a reader who will never
learn a prefix key, which the audience requires.

`/spawn` and `/delegate` are superseded by panes. `/btw` is an ephemeral
one-off with no pane.

## 17. External library

The interface is to be built with an external library. `tview` is the one
proposed, and [0000005](staged/adr-0000005-consider-tview.txt) records what it would
replace and what it would cost.

The line counts in 0000005 measure the frame this redesign replaces rather than a
plan. Two questions in 0000005 are still open and both are checkable before
anything is written: what the byte-level tests become, and whether `bmake
crossbuild` still passes for every target with no cgo.

## 18. What is open

Everything below is a decision that has not been taken.

- Which region yields first when the terminal is short, and how many rows each
  takes.
- What rings the pane bell.
- Whether the pane bar scrolls or folds.
- The `Ctrl+B` command set, and whether the prefix is escapable.
- Whether the prompt belongs to the focused session.
- The clipboard path, and whether copy is owned by the session or the terminal.
- Whether `/copy last` exists, and whether a fenced block is numbered per reply
  or per session.
- Where the message queue lives.
- What `%SOME_IDENTIFIER%` is.
- The set of thinking phrases beyond the one given.
- How long an input error stays.
- Which tools get an icon.
- The member name for the commit attribution in `.orcli.json`.
- The permissions database schema.
- How many history entries are kept, whether a long one is shortened, whether
  the history is saved with the session, and whether a bare command is
  remembered.
- What makes a shell-looking line a no-op, and what "directly in response to a
  question" means, which is the only exception to it.
- The business scope, which is outside this unit and has selected no niche, no
  recurring task, no supported action set and no pricing model.

## 19. Verification

**The Terminal.app workflow has never been run.** Every test in this area draws
to a buffer, and a buffer is not a terminal. Whether the regions feel right,
whether the prefix is reachable, whether scrolling and copying work together,
and whether a mouse wheel behaves on the target machine, are all unverified.