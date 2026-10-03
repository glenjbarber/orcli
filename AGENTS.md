# AGENTS.md

Machine instructions for working on this repository. Nothing here is private to
any person; the file is written to be published with the source.

## Before anything else

Read `.OPENROUTER.md` before making any change. It holds the operating rules for
this tree and they are not restated here, deliberately: a second copy is a copy
that drifts. Where this file and `.OPENROUTER.md` appear to disagree, `.OPENROUTER.md`
decides, and the disagreement is a bug in one of the two.

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

Output goes to `staged/bin/`, worktrees to `worktrees/`, and both are ignored.
Never write a build product beside a source file.

Worktrees are named `{feature,bug,security}/{three-word-summary}`.

## Testing

Every package carries tests. A change to behaviour that arrives without one is
incomplete, and the test is a check on the decision rather than on the line that
carries it.

Tests that need a real dependency should skip rather than fail when it is absent,
so a machine without git or SQLite can still run the suite.

`go test` alone is not the gate. `bmake check` is, because it runs the race
detector and that is what catches a session or a spinner.

## The work log and its lock

A worker records what it is doing in `.orcli.log` at the root of its own worktree,
and takes `.orcli.log.lock` beside it before every write. The log and the lock are
ignored, untracked and never committed, at every depth, since the ignore rules use
a bare name.

**`read_file` needs no lock.** Only a write does. A read takes nothing and blocks
nothing.

**Before writing, check for the lock.** If it is there, do not write and do not
proceed with the write that needed it: say that the ledger is held, name the lock,
and carry on with the rest of the task or stop. A lock older than the work it
belongs to is a lock left behind, and it is reported rather than broken.

**The entry carries four fields**, three of them the abridged view and the fourth
the state of the work rather than of the reader:

```
Module: 0
Worker ID: 0
Merged: notready
Status: <what this worker is doing, in one line>
```

**`Merged` is one of six values**, and it says whether the work on this worktree's
branch has landed on the main line. It is not a boolean, because "did you merge it"
and "could you merge it" and "are you merging it" are three different questions
and a pair of booleans cannot hold all three without being ambiguous in one of
them.

| Value | Means |
| --- | --- |
| `n/a` | This worker wrote no branch. A change made on the main line, or an observation with nothing committed, has no merge to wait for and saying `notready` about it would report a problem that does not exist. |
| `notready` | The branch exists but the work is not finished. Commits may be there or not; the unit is incomplete. This is the value at the start of a piece of work, since a branch that has just been created is not ready by any reading. |
| `ready` | The unit is complete, the gates pass, and it is waiting on a decision to merge. Nothing further will happen to it without one. |
| `inprogress` | The merge is running now. It is the value to write while a merge is in flight and not after, since a worker that wrote `ready` and then merged has described a moment rather than a state. |
| `pending` | A merge has been refused or has failed on a conflict, and the decision is owed. Distinct from `notready`: the work is finished and what is outstanding is a resolution, not more writing. |
| `started` | The branch has been created and work has begun, but nothing has been committed and nothing is finished. Distinct from `notready`, which covers a branch holding work that is incomplete rather than a branch holding none. |
| `done` | The work is on the main line. A worker that writes `done` is finished, and a reader seeing `done` knows nothing is outstanding from this worker. |

**A worker moves forward through them and no further back.** `notready` and
`started` are early, `ready` is the point of asking, `inprogress` covers the merge
itself, and `pending` and `done` are the two ways it ends. Going backwards means
the work reopened, which is a thing to say out loud rather than to express by
rewriting an earlier word.

**`done` is written by the worker whose work merged, not by whoever ran the merge.**
A merge carried out in the main worktree on another worker's behalf leaves that
worker's entry saying `ready` until it writes `done` itself, which is correct: the
entry is the worker's account of its own work, and a worker that reported itself as
merged on the strength of somebody else's action would be reporting an inference
rather than a fact.

**The entry is written on start and again at the end**, so a reader arriving
mid-session sees a worker running and a reader arriving after sees one that has
stopped. An entry written only at the start is a record of intention, and a ledger
whose entries describe what a worker meant to do is not a ledger.

## Committing

One unit per merge, on a branch, merged with `--no-ff`, so the unit is visible in
the history rather than flattened into the main line.

Implementation, documentation, and tests are separate commits, in that order.
A commit explains what changed and why the change was made rather than what the
code does.

Never use `git add -A`. Stage by path.

Commit subjects are one line, imperative, and under 72 characters where that is
possible.

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

Where the two disagree, one of them is wrong and the disagreement is a bug to be
fixed rather than a nuance to be noted. Name the file that is wrong in the commit
that fixes it, so the next reader does not have to work out which one it was.