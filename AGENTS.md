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
internal/openrouter/     HTTP client, SSE stream parser, wire types
internal/config/         JSON configuration, colour writer, trusted directories
internal/saved/          SQLite session store, autosave naming
internal/tools/          tool execution: filesystem, git, shell
.github/workflows/       CI definitions
Makefile                 developer targets, written for BSD make and GNU make alike
```

`internal/` is the module boundary. Nothing is importable from outside it.

The four packages carry the code that exists today, and they import one another in
no direction, because each is a leaf that the wiring layer is not yet written to
join. A dependency that runs the other way is what lets a model tool reach the
interface that drew it, so the direction is worth stating when there is one; for
now the honest statement is that there is none, and a new edge needs a reason.

The interface, the main package, the bootstrap loader, the completion engine and
the clipboard encoder are described in the design documents under `staged/` and
are not in the tree yet. Treat those documents as the specification for what a
package will do, not as a description of a package that exists. `staged/` is
ignored and holds build output beside that material, so nothing under it is
committed and `go build ./...` may pick up a stray file placed there.

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

`bmake build` reports that there is no main package and places no binary, and that
is an ordinary message rather than a fault: the library packages still compile.

Worktrees are named `{feature,bug,security}/{three-word-summary}`.

## Testing

Every package carries tests. A change to behaviour that arrives without one is
incomplete, and the test is a check on the decision rather than on the line that
carries it.

Tests that need a real dependency should skip rather than fail when it is absent,
so a machine without git or SQLite can still run the suite.

`go test` alone is not the gate. `bmake check` is, because it runs the race
detector and that is what catches a session or a spinner.

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
diagnostic.** Every string bound for a message is filtered for it first.

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
writing anything.

**The credential file is mode 0600 and every writer is named.** There is a fixed
set of writers, each in `internal/config`, and a command layer that writes bytes
itself is a bug.

## Where decisions are recorded

`DESIGN.md` records what the system is and how its parts fit. This file records
what to do and where things are. Neither records why a decision was taken.

The rule for adding to them: `DESIGN.md` gets a change that alters what the
system does, and this file gets a change that alters how to work on it. A
rationale belongs in neither and in a commit message.

Where the two disagree, one of them is wrong and the disagreement is a bug to be
fixed rather than a nuance to be noted. Name the file that is wrong in the commit
that fixes it, so the next reader does not have to work out which one it was.