# AGENTS.md

Machine instructions for working on this repository. Nothing here is private to
any person; the file is written to be published with the source.

## Before anything else

This file is read by default. It holds the operating rules for this tree, and
no other file holds them. The rules that were previously kept in `.orcli.md`
live here now, and `.orcli.md` is no longer read by anything in the repository.

Where this file and the code appear to disagree, the code is what exists and
this file is what was meant, so the disagreement is a bug in one of the two.

## How to work here

The same rules govern the files and the replies.

**Commands are run one at a time.** Evaluate the output of a command before
running the next one, and do not string commands together.

**Prose is direct.** American English. No contractions. No em dashes. Straight
double quotes and straight apostrophes only. No long sentences. Format for
human readability. A human-readable result goes in a fenced `text` block.

**Replies are written for the reader.** Interact in the first and second
person. A mid-task reply covers what was found, what changed, what was
verified, and what is needed next. Questions go last, not in the middle.

**The backlog is held and kept short.** It runs to no more than four active
items. Restate it whenever a reply depends on it, since the reader may not have
seen it.

**A serious finding stops the work.** When something found is bad enough to
stop, the reply is that finding and one question. It is not a backlog item and
it is not followed by other work. Assume the reader does not have the full
picture, since the real picture is larger and not yet fully understood.

**The work ends green.** A repository is never left in a known-broken state.
Build, lint and test gates pass when the work ends, and a failure that cannot
be settled is reported rather than left for the reader to find.

**Push only on the exact phrase.** Never push to a remote unless told
otherwise with the words `YES-I-REALLY-MEAN-IT`. The repository is pushed by
hand.

## Where things live

```
cmd/orcli/                main package: flags, trust gate, wiring
internal/openrouter/      HTTP client, SSE stream parser, wire types
internal/config/          JSON configuration, colour writer, trusted directories
internal/tui/             the interface: the log, the session, the terminal check
internal/saved/           SQLite session store, autosave naming
internal/tools/           tool execution: filesystem, git, shell
internal/clipboard/       OSC 52, written to the terminal rather than run
.github/workflows/        CI definitions
Makefile                  developer targets, written for BSD make and GNU make alike
```

`internal/` is the module boundary. Nothing is importable from outside it.

The packages carry the code that exists today, and they import one another in no
direction, because each is a leaf. `main` is the place that direction changes, and
it is allowed to: `main` may reach `config`, `openrouter`, `tools`, `saved`,
`clipboard` and `tui`. The direction that must not appear is `tui` reaching
`tools`, since that is what would let a model tool reach the interface that drew
it. A new edge anywhere else needs a reason.

`internal/tui` holds the log, the session and the terminal check. The rest of the
interface, the line editor, the palette, the command table and the terminal
control, are described in the design documents under `staged/` and are not in the
tree. Treat those documents as the specification for what a package will do, not as
a description of a package that exists. `staged/` is ignored and holds build output
beside that material, so nothing under it is committed and `go build ./...` may
pick up a stray file placed there.

## Building

```
bmake build        the binary into staged/bin/orcli
bmake test         the suite
bmake check        the suite under the race detector
bmake lint         gofmt check and go vet
bmake crossbuild   compile and vet every supported target
bmake clean        remove staged/bin/
```

`bmake` and `make` are both supported and both are expected to work; the
`Makefile` avoids every construct that is not common to both. `make clean` removes
build output and never touches `worktrees/`, which holds checkouts of this
repository.

Output goes to `staged/bin/`, which is ignored.
New worktrees sit outside this repository, at `/Users/gjb/Documents/work/orcli-worktrees/<type>-<name>`.
Never write a build product beside a source file.

Worktrees are named `<type>-<name>`, for example `docs-session-implementation-note`.

## Testing

Every package carries tests. A change to behaviour that arrives without one is
incomplete, and the test is a check on the decision rather than on the line that
carries it.

Tests that need a real dependency should skip rather than fail when it is absent,
so a machine without git or SQLite can still run the suite.

`go test` alone is not the gate. `bmake check` is, because it runs the race
detector and that is what catches a session or a spinner.

## Handoffs and task status

Use the relevant Notion handoff as the source of task instructions and status.
Keep progress and completion updates with that handoff. Do not create or maintain
a local work log or lockfile for task status.

## Committing

One unit per merge, on a branch, merged with `--no-ff`, so the unit is visible in
the history rather than flattened into the main line.

Implementation, documentation, and tests are separate commits, in that order.
A commit explains what changed and why the change was made rather than what the
code does.

Never use `git add -A`. Stage by path.

Commit subjects are one line, imperative, and under 72 characters where that is
possible. They are written in the third person and the passive voice.

Every commit carries the trailer:

```
Co-Authored-By:	Space Bunny Alpha
```

Never push. The repository is pushed by hand.

## Editing

Prefer `write_file` over `sed` and `awk`. A shell tool here writes a backup file
beside the one it edits, and that backup is a stray file nobody asked for.

Commands follow BSD syntax, not GNU. `sed -i ''`, not `sed -i`.

Edit one file in place rather than rewriting it whole. A whole-file rewrite
propagates a mistake into the permanent record, and the fix for one mistake is
another rewrite that can introduce the next one.

## What is deliberately not read from the environment

No environment variable is read for configuration, on the same terms as the API
key. `OPENROUTER_API_KEY` is ignored even when set, because a file shadowed by a
stale value elsewhere is a failure the reader cannot see.

Two exceptions, and only two:

- `PATH`, which the shell tool reads to resolve a permitted program by bare name.
- `TERM`, which the palette reads as a capability rather than as configuration.

A third variable is a bug unless it is on that list.

## Rules that hold across the codebase

**A call always produces a result.** An unknown name, arguments that are not a
JSON object, a body that failed, and a body that panicked are all reported as a
result carrying the error. A call producing no result leaves a turn waiting for
something that never arrives.

**The credential is never read from the environment and never written to a
diagnostic.** Every string bound for a message is filtered for it first. It never
reaches a log row either, since a log is a thing a reader selects out of and pastes
somewhere else.

**Containment is an open descriptor, not a prefix check.** A cleaned path
comparison is defeated by exactly the symlink it cannot see. Subprocesses get no
descriptor and are bounded by comparison, which is a weaker thing and is treated
as such.

**A path outside the working directory is refused.** `..` in any form, an absolute
path, and a symlink pointing out are all refused before anything is opened or
run.

**A file is never re-encoded to be edited.** The configuration file holds a
credential, and decoding and re-encoding it reorders keys, reindents, and
normalises line endings. Edit the bytes, and check the result parses before
writing anything. A single-line file stays on one line.

**The credential file is mode 0600 and every writer is named.** There is a fixed
set of writers, each in `internal/config`, and a command layer that writes bytes
itself is a bug.

**A retry needs a reason to be safe.** A request is made again only while it has
delivered nothing, since nothing spent is nothing to duplicate. A reply that has
begun is a reply that was paid for, and the text before a cut is kept rather than
taken back.

**A row leaves as plain text.** Whatever a model wrote is stripped of the bytes a
terminal would act on, whole sequences rather than the control byte alone, since a
clear-screen that lost only its escape arrives at the reader as visible nonsense.
Tabs and box-drawing figures are kept: a tab is a column of space rather than a
sequence, and a figure is text.

**A terminal is asked, not inferred.** A stat cannot tell a terminal from a
character device that is not one. The answer comes from a termios read, and a
platform with no spelling for it says so rather than guessing.

## Where decisions are recorded

`DESIGN.md` records what the system is and how its parts fit. This file records
what to do and where things are. Neither records why a decision was taken.

The rule for adding to them: `DESIGN.md` gets a change that alters what the
system does, and this file gets a change that alters how to work on it. A
rationale belongs in neither and in a commit message.

A rationale that is worth keeping beyond the commit that took it is a decision
record under `staged/`, one file per decision, with an index beside them. Those
records are numbered so a later one can supersede an earlier one by name.

**A record says on its own header that it has been superseded**, and names what
superseded it. The status line carries `SUPERCEDED` beside the number, and the
replacing record names the record and the part of it under `Supersedes`, so a
reader holding either file learns about the other without going to the index. The
five status values are `accepted`, `proposed`, `UNCONFIRMED`, `SUPERCEDED` and
`rejected`, and `staged/adr-status-vocabulary.txt` defines them and says which
records carry each.

**A superseded record is kept as it was written and is read to find what was
replaced.** It is not edited to agree with its replacement, since a record edited
to agree with the code is no longer a record of what was decided, and it is not
deleted, since the rejected alternative is the thing worth keeping.

Where the two disagree, one of them is wrong and the disagreement is a bug to be
fixed rather than a nuance to be noted. Name the file that is wrong in the commit
that fixes it, so the next reader does not have to work out which one it was.
