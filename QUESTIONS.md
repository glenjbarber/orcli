# Questions

Open decisions for `feature/tui-redesign`, gathered in one place so a reader
can answer them in any order. The design is in `DESIGN.md` and the API is in
`staged/adr-0000010-interface-api.txt` beside
`staged/adr-0000011-editing-keys-and-input-history.txt`. Every question below is
recorded as open in one of those two, and none has been answered in code.

Answers go here, and each one becomes a decision record under `staged/`.

Twenty-two questions have been answered and are struck from this file
rather than marked in place. What was decided is in `ANSWERS.md`, one
record per question, and each became a decision record: 0000012 for the
queue, 0000013 for the identifier in the `.latest` symlink, 0000014 for what a
restore carries, 0000015 for the history, 0000016 for the shed ladder, 0000018 for
the pane bell, 0000020 for the pane bar's overflow, 0000021 for the status bar
fields and wrapping, 0000022 for the prompt's ownership, 0000023 for the
escapable prefix, 0000025 for copy's ownership, 0000026 for `/copy last` and
block numbering, 0000027 for the input error timeout, 0000028 for tool icons,
0000029 for the thinking phrase set, 0000030 and 0000031 for the permissions schema
and its deduplication, 0000033 for a pane's default (using 0000032's `/begin`),
0000034 for the commit attribution field, 0000035 for the shell no-op rule, 0000036
for crash survival, 0000037 for the pane bar's place in the shed ladder, 0000038
for `/pane` listing every open pane, 0000039 for `/model` completion, and 0000040
for the tool-icon specifics. The numbering below starts again at 10, so a
question is named by the number it was given when it was open.

## Blocking the keys and the prefix

**10. What is the `Ctrl+B` command set?**

Nothing has been designed. The `tmux` convention would give next pane,
previous pane, new pane, close pane and rename. A reader who knows `tmux`
already has a set in mind, and this unit should not guess at it.

The prefix itself was reconsidered and reaffirmed as `Ctrl+B`, unchanged
from 0000007; no record was written for keeping it, since nothing changed. The
sub-key set remains exactly this question, open and deferred.

## Blocking copy and paste

**12. What is the clipboard path on macOS Terminal.app?**

[0000024](staged/adr-0000024-pbcopy-and-osc-52-are-ruled-out-on-terminal-app.txt),
status `UNCONFIRMED`, narrows this without closing it. `pbcopy` is ruled out
by a standing policy against arbitrary pasteboard writes, not by Secure
Keyboard Entry, which turns out to be unrelated to the clipboard entirely.
OSC 52 is ruled out on Terminal.app specifically, verified unsupported
there regardless of any setting. The reader wants a clipboard path and has
not yet decided what it is; `pbpaste`, for reading only, is not ruled out by
the standing policy as stated, but is not confirmed as the answer either.

## Outside this unit

**23. What is the commercial scope?**

The roadmap is a product and commercial assessment, not a code audit. It names
unresolved P0 questions about the target customer, the recurring workflow,
API feasibility, approval and correction cost, and whether the product saves
enough effort to justify setup and support. P1 covers credential onboarding,
authoritative business data, completion versus submission states, retry
safety, support cost, repeat use and acquisition economics. P2 covers
positioning, team and agency requirements, open-source monetization and
integration maintenance. No niche, recurring task, supported action set,
pricing model or commercial scope has been selected.

## Backlog

Raised during the Q/A rather than against a numbered question. Not yet a
question with its own context, and not yet ratified.

**30. Read the OpenRouter API docs to find what features can be
implemented.**

**31. Notion API integration.**

**32. Investigate the shared documentation directory.** `adr-index.txt` and
related docs appear to be created under a shared directory rather than under
`doc/`; the same behavior has been seen on two machines, and the cause is
unknown. Keep the move pending investigation. If the intended relocation is
confirmed, use `git mv` to preserve file history.
