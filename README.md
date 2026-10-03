# orcli

A terminal client for the [OpenRouter.ai](https://openrouter.ai) API.

`orcli` is built around one substantial feature: an interactive, full-screen chat
interface in the manner of Codex, ChatGPT, Claude, and Perplexity. Around it
sits a thin surface of non-interactive commands (`version`, `help`, and the flag
parsing for `--bootstrap` and `--mouse`).

The project is pre-release. `PORTVERSION` is `0.0.0-dev`, and no release has
been scoped.

## Design overview

A full account of the design lives in `DESIGN.md`. It is written from the code
and the shape of its history, and it records what the system is and how its
parts fit, where `AGENTS.md` records why individual decisions were taken. The
summary below is a short orientation, not a substitute.

### Packages

```
cmd/orcli/     main package: flags, trust gate, wiring
internal/bootstrap/     --bootstrap documents (.md, .json, .db) and symlink resolution
internal/complete/      Tab completion engine
internal/config/        JSON configuration, colour writer, trusted directories
internal/openrouter/    HTTP client, SSE stream parser, wire types, cost ledger input
internal/saved/         SQLite session store, autosave naming
internal/tools/         tool execution: filesystem, git, shell
internal/tui/           the interface: rendering, input, session, panes
```

`internal/` is the module boundary, and the dependency direction is one-way and
deliberate:

```
main → config, bootstrap, tui
tui   → openrouter, tools, config, complete, saved, bootstrap
tools → openrouter        (wire types only)
openrouter → (stdlib)
```

`tools` imports `openrouter` only for the wire types. It never runs a request;
`tui` runs the requests and the tools. That is why the model tools cannot reach
the interface that drew them.

### Rendering

The interface is driven through `syscall` directly, with no terminal library,
because the copyable-text requirement needs control of mouse reporting that
higher-level libraries take by default.

`Frame` is the whole interface at one moment. Its load-bearing contract is that
every row leaves the renderer as plain text, with the bytes a terminal would act
on removed, and colour travels *beside* the rows as spans rather than inside
them. Rows can therefore be measured, folded, scrolled, searched, and copied
without knowing about colour at all.

The frame is vertically partitioned, and when space runs short the header yields
before the prompt does, giving up rules first, then blanks, then the title. A
rule is never drawn without the blanks on both sides of it. Widths are counted
in display columns, not bytes or runes, so a box-drawing rule is not reported
three times too wide.

### Input

The line editor reads blocks rather than bytes, so a whole mouse report is
recognised before it can be read as a key. Key sequences are consumed from their
shape rather than from a table of lengths. Above the prompt, pasted rows, queued
rows, a notice, a hint row, a confirm box, and the prompt are each budgeted
against the rows left after the prompt is accounted for, so none of them can push
the prompt off the bottom of the screen.

### Conversation

Retention is decided in one place: the record function checks an ephemeral flag
and returns. Everything that promises not to record is implemented by setting
that flag, because two places could disagree and the disagreement would be a
session that appeared to record nothing while keeping everything.

The bootstrap instructions are a system turn, and a reset keeps them. A
tool-using turn is committed once, at the end, and only on a clean exit, so a
turn the reader stopped leaves no tool result behind for the model to be told
about as though it had asked for one and been answered.

### Tools

From the package documentation: a call always produces a result. An unknown
name, arguments that are not a JSON object, a body that returned an error, and
a body that panicked are all reported as a result carrying that error, because a
call producing no result at all leaves the turn waiting for something that never
arrives, and a turn that waits is reported by a reader as a hang rather than as
a fault.

Filesystem containment is an open descriptor, not a prefix check. A prefix check
on a cleaned path is defeated by exactly the symlink that a string comparison
cannot see. Git and shell use subprocesses, so they compare the resolved path
against the resolved working directory instead. Neither ever reaches a shell:
arguments go to the process as an array.

Permission is three levels in increasing precedence: an allowlist bounds what may
be proposed, an approval mode settles every call a file rule does not, and a file
rule under `OPENROUTER_TOOLS` settles a call whatever the mode says. The nearest
enclosing rule decides, not the union.

Tools are offered under one condition, that the conversation is recording. A tool
acts on the reader's behalf, and a mode whose promise is that nothing is recorded
cannot hand the model a hand that acts.

### API client

Standard library only. The terminating `[DONE]` marker is required rather than
assumed, because a stream that ends without it was cut short and must not be
presented as a complete reply. Failures are reported through the callback rather
than as a return value, so the text that arrived before a failure is kept. Tool
call fragments are reassembled by the transport and joined on the wire index
rather than arrival order, since the index is the only field every fragment
carries. Usage and cost are pointers, because a reported zero and an absent value
are different.

### Configuration

JSON, because the standard library has no YAML parser and a configuration file is
not a place where a dependency is worth taking. The API key is never read from
the process environment, an environment variable of the same name being ignored
even when set. The file must be `0600`; any other mode is a hard startup failure,
because a permissive mode leaks the credential silently.

The colour writer is the interesting one. It does not re-encode the file. It
changes the value of a top-level member, or adds one in the file's own style,
matching indentation, line separator, and colon spacing, and preserving key
order, whitespace, escapes, and a missing final newline.

### Compaction and cost

Compaction is checked at 75% of the window before every request, since a request
past the window is refused outright and the turn is lost with it. The summary is
a system turn carrying a marker, so a second compaction replaces it rather than
summarising a summary. The opening instructions survive and are held out of the
summary request.

Cost is a session ledger in memory, summed from the usage event of every response
the session makes. It belongs to the session and not to a conversation, since
money spent is not undone by clearing the conversation it was spent on.

### Error handling

The recurring shape: a missing precondition is an ordinary message rather than a
refusal, so the interface still opens and reports it; a dangerous precondition is
a hard failure; a failure with partial success keeps the partial; a cancellation
the reader asked for is not a fault; and every diagnostic is bounded and
credential-filtered, because it is shown in the pane and ends up in the terminal
scrollback.

## Build

The design records a `Makefile` written in the syntax common to BSD make and GNU
make, and a `make crossbuild` target that compiles and vets every supported
target, which exists because the terminal layer names ioctl requests that differ
between the BSD family and System V and nothing else would notice.

## Build status

The repository at present holds documentation only. The `DESIGN.md` document was
written from a code tree that is not yet part of this repository, so the packages,
commands, and targets described above are the design as recorded, not a
description of code currently in the tree.

## License

See `LICENSE`.
