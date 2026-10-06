# Answers

Ratifications for `feature/tui-redesign`, in the order the questions are
numbered in `QUESTIONS.md`. One record per question, so a reader can see what
has been settled and what has not.

A ratified question becomes a decision record under `staged/`, and the
question is struck from `QUESTIONS.md` rather than marked as answered in
place. A question left open is left alone.

## 1. Where does the message queue live?

**Ratified.** In memory in the session, behind a small interface, with SQLite
available later behind the same interface.

**The related question, is a queued message durable before it is sent, is
answered as no.** It becomes durable when it is sent, through the save path the
session already uses.

**Record.** [0000012](staged/adr-0000012-the-queue-is-in-memory-behind-an-interface.txt)

**Cost accepted.** A crash loses what the reader typed and had not sent. The
loss is bounded to the queue, since the conversation so far has already been
recorded by the autosave that runs after each clean turn.

## 2. What is `%SOME_IDENTIFIER%` in the `.latest` symlink?

**Ratified.** The session's working directory, slugified, so a reader gets one
latest session per project and a reader working in two projects gets two.

**The symlink is named `<slug>.latest`** and sits beside the session file it
names.

**Record.** [0000013](staged/adr-0000013-the-latest-session-is-keyed-on-the-directory.txt),
following the autosave naming rules already in `internal/saved`: replace rather
than strip a character a filename cannot carry, escape a literal escape first,
shorten from the front and mark when the path is too long.

**Cost accepted.** A reader who renames a directory loses the link to their
last session there, since the identifier is derived rather than stored.

## 3. What context survives a restart?

**Ratified.** The whole conversation, plus the model, the approval mode and
the pane permissions.

**The opening instructions are not restored.** They are read again from the
working directory, since a reader who moved the project has a different set and
restoring yesterday's copy would answer as yesterday's program.

**Record.** [0000014](staged/adr-0000014-a-restored-session-carries-the-whole-conversation.txt)

**Cost accepted.** A restore is not a pure file operation. It depends on the
directory being the one the reader is in, since the instructions come from
there, and a reader who restores standing somewhere else gets that directory's
instructions with this conversation. That is named in the record rather than
designed around.

## 4. How many history entries are kept?

**Ratified.** One hundred, exact text, nothing shortened. The hundred most
recent, and the oldest falls off.

**The history is saved with the session and restored with it.** The reader said
the persistence half can be carried by `/autosave` and `/autocompact`, and
neither is built, so what each one trims or writes is its own work rather than
part of this decision.

**A bare command word is remembered like any other line.** A reader who does
not want a command offered back uses `Ctrl+U` or walks past it.

**Record.** [0000015](staged/adr-0000015-the-history-is-one-hundred-exact-entries.txt)

**Bound on count, not on length.** A pasted paragraph is a full-length entry,
so the cost is bounded by what the reader pastes rather than by the count.

## 5. Which region yields first when the terminal is short?

**Ratified on five of six regions.** The sixth is the pane bar, which the
reader withdrew while updating the documentation, and a question about a
region the layout no longer has cannot be closed by deciding its shed order.

**Record.** [0000016](staged/adr-0000016-the-shed-ladder.txt)

**The ladder, in order.** The scrollback and mouse indicators go first. Then
the fields of each bar, from the right. Then a bar drops whole. Then the live
window, which keeps at least one row and goes no lower than that.

**What is held.** The prompt is never dropped and never clipped, and is drawn
last so it is the row that survives.

### Reasoning

**The indicators go first because they are the cheapest thing to lose.** They
report a mode rather than an activity, and both are worth nothing in the state
where the terminal is short. `[scrollback]` exists to explain why the screen is
not moving, which is worth having but not worth a row above a field the reader
came to read.

**The bars go before a field within them, which is the order the reader
settled first.** Status bar 1 sheds Verbosity, then Bell. Status bar 2 sheds
Approval, Stealth, Autosave, Context, Hostname. A bar drops whole at one field
left, so a bar is never a single floating field, and whether a field is present
does not depend on how the bar happens to be sized.

**The live window yields last of the shed regions and keeps a row.** Zero rows
cannot tell a program thinking from a program stopped, and one row is the whole
difference between idle and dead.

**A shed region returns when the terminal grows.** The layout pass runs on
every resize, so a region returning is the absence of a shed rather than a
second piece of work.

### Cost accepted

A reader at a narrow terminal loses the two things that tell them the terminal
is the problem: the scrollback indicator and the mouse indicator. A reader who
does not notice has a live window and a prompt and no way to know why the screen
is smaller than they left it.

The pane bar, had it stayed, would have sat in this ladder below the bars. With
it gone there are six regions and the ladder above is the whole of it.

### What this leaves open

Nothing. Every region on the current screen has a place in the ladder.

### A note on the record number

0000016 is written for a screen whose pane bar is gone, so it describes six regions
while `DESIGN.md` and `staged/adr-0000010-interface-api.txt` still draw seven. The
pane bar withdrawal and the pane zero broker direction arrived after the
question was asked, so the record describes what the reader answered and names
the disagreement rather than silently drawing a screen that does not exist.

**The pane bar's withdrawal, noted above, is reversed.** [0000019](staged/adr-0000019-the-pane-bar-returns.txt)
restores it as a seventh region, which closes the disagreement with
`DESIGN.md` and `staged/adr-0000010-interface-api.txt` from the other side: the
seven they draw is current again. 0000016 itself is not rewritten; it stands as
the record of the shed order decided while the pane bar was out, and the gap
it left for the pane bar's place in that ladder is reopened as question 25.

## 7. Does the pane bar scroll or fold?

**Ratified.** Neither. When there are more panes than the bar has columns
for, it truncates silently: as many pane numbers as fit, left to right, with
nothing marking that more exist past the edge.

**Record.** [0000020](staged/adr-0000020-the-pane-bar-truncates-rather-than-scrolling-or-folding.txt)

**Cost accepted.** A pane past the edge, including the focused one if focus
moves there, is invisible in the bar, and the bar gives no sign that it is
hiding anything. How a reader reaches a truncated-off pane is not answered
here and is reopened as question 26.

## 8. What are the status bar fields, and which fall off?

**Ratified.** Mostly already settled before this question was closed: the
fields and the shed order are `SAVED.md`'s and `adr-0000016`'s, unchanged.
**The one piece 0000016 left unstated is answered here: neither bar wraps.** A
bar sheds fields from the right, one at a time, then drops whole, on a single
row, and never grows to a second row to keep a field.

**Record.** [0000021](staged/adr-0000021-the-status-bar-fields-and-that-neither-bar-wraps.txt)

**Cost accepted.** A reader at a narrow terminal loses a field outright
rather than seeing it move to a second row.

## 6. What rings the pane bell?

**Ratified.** The bell rings for an error or a reply that needs an answer, and
only in a pane that is not focused. A plain reply arriving does not ring it,
even in a background pane, and neither does anything in the focused pane.

**Record.** [0000018](staged/adr-0000018-the-bell-rings-for-error-and-needs-an-answer.txt)

**Cost accepted.** A reader who wants to know the instant any background pane
finishes, even cleanly, does not get that from the bell. The pane bar's status
colour is where that is shown instead.

## 9. Does the prompt belong to the focused session?

**Ratified.** Yes. The current line, caret position, and input history are
the session's own, the same as the queue (1) and the receive queue.

**Record.** [0000022](staged/adr-0000022-the-prompt-belongs-to-the-focused-session.txt)

**Cost accepted.** None beyond the implementation shape: one line-editor
instance per session rather than one shared by the interface.

## 11. Is the prefix escapable?

**Ratified.** Yes, by pressing it twice. `Ctrl+B` `Ctrl+B` inserts one
literal `Ctrl+B` byte; `Ctrl+B` followed by anything else reads as a
command. This is `tmux`'s own resolution of the same problem.

**Record.** [0000023](staged/adr-0000023-the-prefix-is-escaped-by-pressing-it-twice.txt)

**Cost accepted.** The literal byte costs two keystrokes rather than one.

**A separate question, whether `Ctrl+B` itself should be replaced with a
less commonly bound key, was raised and withdrawn.** `Ctrl+B` stands,
unchanged from 0000007; no record exists for keeping it, since nothing
changed. See question 10, still open, for the sub-key set.

## 12. What is the clipboard path on macOS Terminal.app with Secure Keyboard
Entry?

**Not ratified.** `pbcopy` and OSC 52 on Terminal.app are both ruled out,
but the path itself is not decided; the reader wants one and has not chosen
it yet.

**Record.** [0000024](staged/adr-0000024-pbcopy-and-osc-52-are-ruled-out-on-terminal-app.txt),
status `UNCONFIRMED` — a new status value, added to
`staged/adr-status-vocabulary.txt`, for a record that narrows a question
without closing it.

**What was corrected along the way.** Secure Keyboard Entry was checked and
found unrelated to the clipboard; it isolates keystrokes from
accessibility APIs and touches nothing about `NSPasteboard`, `pbcopy`, or
an OSC 52 write. OSC 52 was checked against the `can-i-use-terminal`
compatibility matrix and found unsupported by Terminal.app outright, not
merely at risk of being ignored as `adr-0000005` had hedged.

**Still open.** What the actual clipboard path is. This remains question
12, narrowed rather than struck.

## 13. Is copy owned by the session or by the terminal?

**Ratified.** The session. The copied text is session state, the same
kind of thing the queue and the prompt already are, and `/paste` reads it
from there regardless of what 12 eventually settles about an OS-clipboard
path.

**Record.** [0000025](staged/adr-0000025-copy-is-owned-by-the-session.txt)

**Cost accepted.** `/copy` never reaches the system pasteboard for pasting
into a different application; that remains question 12's to answer.

## 14. Does `/copy last` exist, and is a fenced block numbered per reply or
per session?

**Ratified.** Blocks are numbered per reply, restarting at 1 each time,
which is what makes `/copy N M` a two-argument command rather than one with
an inert parameter. `/copy last` exists and spells as `N 0`, not `N 1` as
the question framed it — `SAVED.md` already defines `N` as "replies back,"
so the current reply is zero back.

**Record.** [0000026](staged/adr-0000026-copy-last-spells-as-n-zero-and-blocks-are-numbered-per-reply.txt)

**Cost accepted.** None beyond the correction to the question's own framing
of what `last` would spell as.

## 15. How long does an input error stay in the input field?

**Ratified, as a placeholder.** Three seconds. No derivation; the question
named its own answer as provisional before it was given.

**Record.** [0000027](staged/adr-0000027-an-input-error-clears-after-three-seconds.txt)

**Cost accepted.** Expected to be wrong and revised once watched on a real
terminal.

## 16. Which tools get an icon, and what is the icon when the terminal
cannot draw it?

**Ratified.** One icon per category (`fs`, `git`, `shell`, matching
`internal/tools`), not per action. Each category icon carries three
states: running, succeeded, failed, shown as a result marker (green check
or red x) once the call completes. The fallback, when the terminal cannot
draw an icon, is the bracketed text tag carrying the same three states as
text (`[fs] Running...` becoming `[fs] Done` or `[fs] Failed`), not a
static, state-blind tag.

**Record.** [0000028](staged/adr-0000028-tool-icons-are-per-category-with-a-running-and-a-result-state.txt)

**Cost accepted.** None named beyond what is left open below.

**Still open, as backlog items 27-29.** The specific glyphs for `fs` and
`git`. The running-state words and their timer. The plain-text spelling of
success and failure for a terminal that also cannot draw the check or the
x.

## 17. What is the set of thinking phrases?

**Ratified.** Twenty phrases: `thinking` and
`No Segmentation Fault (no core dumped)`, already settled, plus eighteen
more generated to match their register and accepted as a batch. The full
list is in the record.

**Record.** [0000029](staged/adr-0000029-the-thinking-phrase-set.txt)

**Cost accepted.** None; a flavour choice rather than a design tradeoff.

**Still open.** How a phrase is chosen each time the timer fires.

## 18. What is the permissions database schema?

**Ratified.** `permissions(pane, path, verdict, moment)`. The verdict is
one column with two values, `allow`/`deny`, not two columns, to rule out a
state where both or neither are set. A removal is a new row, never a
deletion — the table is an append-only ledger, matching every other place
history is kept in this project.

**Record.** [0000030](staged/adr-0000030-the-permissions-table-is-an-append-only-ledger.txt)

**Cost accepted.** The table grows without bound; reading the current
verdict means finding the latest row for a pair rather than a direct
lookup.

**Extended since.** The growth question 0000030 left open is settled by
[0000031](staged/adr-0000031-the-permissions-ledger-is-deduplicated-after-each-session.txt):
the ledger is deduplicated once, after each session, keeping only the
latest row per `(pane, path)`.

## 19. What does a pane default to, and what does escalating from a pane
change?

**Ratified.** An ordinary new pane defaults to `ask`, the least-privilege
value of the tree's three. Escalating changes only that pane's own ledger
rows, never the session-wide mode and never another pane.

**A new command, `/begin`, was introduced along the way and settled
first.** It forks a pane (the caller stays open), hands the new pane a
summary note rather than the full transcript, and seeds its permissions
with a copy of the caller's current verdicts at the moment of the fork.
Record [0000032](staged/adr-0000032-begin-forks-a-pane-with-a-handoff-note-and-copied-permissions.txt).
**A pane created by `/begin` does not default to `ask`**; it starts from
that copied ledger instead.

**Record.** [0000033](staged/adr-0000033-a-pane-defaults-to-ask-unless-begun-from-another.txt)

**Cost accepted.** A pane begun from another before an escalation was made
in the source does not receive it retroactively; the copy is taken once, at
the fork.

## 20. What is the member name for the commit attribution in `.orcli.json`?

**Ratified.** `commit_attribution`, distinct from the existing
`attribution_id` field (which is OpenRouter's own request attribution, not
a git co-author). Its value is a map keyed by provider, then by model, down
to the attribution string read with `git config get`.

**Record.** [0000034](staged/adr-0000034-commit-attribution-is-a-map-keyed-by-provider-then-model.txt)

**Cost accepted.** The one nested field in `.orcli.json`; every other field
there is a flat scalar or list.

## 21. What makes a shell-looking line a no-op, and what does "directly in
response to a question from the model" mean?

**Ratified.** A line is shell-looking if its first token resolves to an
executable via the same PATH lookup the shell tool already uses — no new
heuristic or word list. The exception is a one-shot flag: if the model's
immediately preceding message ends in a question mark, the next line is
let through regardless of content, and the flag clears.

**Record.** [0000035](staged/adr-0000035-shell-looking-is-path-resolution-and-the-exception-is-a-one-shot-flag.txt)

**Cost accepted.** A line whose first word happens to name an executable
for an unrelated reason is swallowed as shell-looking even when not meant
as a command.

## 22. What survives a crash, and where is it written?

**Ratified.** Every row as it arrives, not only what the last autosave
checkpoint captured. Written to a plain, append-only file, one per
session, flushed after every row, beside the session file 0000013 already
locates — not the SQLite store, on the same cost reasoning 0000012 already
gave for the queue.

**Record.** [0000036](staged/adr-0000036-a-plain-append-only-log-beside-the-session-survives-a-crash.txt)

**Cost accepted.** Recovery means reading two sources, the autosaved state
and the plain log, rather than one.

## 25. Where does the pane bar sit in the shed ladder, now that it has
returned?

**Ratified.** Third: after the scrollback and mouse indicators, before
either status bar sheds a field. Costlier to lose than an indicator, since
it is the only place pane identity is shown at all, but cheaper than the
bars, which carry the reader's present activity.

**Record.** [0000037](staged/adr-0000037-the-pane-bar-sheds-third-after-the-indicators.txt)

**Cost accepted.** A reader at a narrow terminal loses pane visibility
before either status bar loses a single field.

## 26. How does a reader reach a pane the bar has truncated off the edge?

**Ratified.** `/pane` with no argument lists every open pane number in the
live window, independent of the bar. The bar's own truncation (7) is
unaffected.

**Record.** [0000038](staged/adr-0000038-pane-with-no-argument-lists-every-open-pane.txt)

**Cost accepted.** None beyond the keystroke itself; this does not make the
bar's truncation less lossy, it gives the reader an escape from it.

## 24. Tab-completion for `/model` switching.

**Ratified, as two mechanisms.** Every keystroke shows an inline
suggestion of the most specific match, drawn from the cached catalogue.
Tab, pressed explicitly, cycles through a live list fetched from the API.

**Record.** [0000039](staged/adr-0000039-model-completion-is-inline-suggestion-plus-a-live-tab-list.txt)

**Cost accepted.** The two mechanisms can disagree if the cache is stale
relative to the live list. Tab costs a network round trip on every press.

## 27-29. Tool icon glyphs, running words, and plain-text results.

**Ratified.** Glyphs: `shell` = 🖥️, `fs` = 📄, `git` = 🌿, all the same
terminal cell width. Running words: `Running...` / `Working...`,
alternating on 0000029's existing timer. Plain-text results: `Done` /
`Failed`, formalizing what 0000028 already used as its own examples.

**Record.** [0000040](staged/adr-0000040-tool-icon-glyphs-running-words-and-plain-text-results.txt)

**Cost accepted.** None beyond what 0000028 already named.