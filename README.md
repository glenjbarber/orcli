# orcli

![OS](https://img.shields.io/badge/OS-FreeBSD-red.svg?logo=freebsd&logoColor=white)

![Architecture](https://img.shields.io/badge/arch-amd64-blue)
![Architecture](https://img.shields.io/badge/arch-arm64-blue)
![Architecture](https://img.shields.io/badge/arch-aarch64-blue)

[![License](https://img.shields.io/github/license/glenjbarber/orcli?color=blue)](LICENSE)

![Go Version](https://img.shields.io/github/go-mod/go-version/glenjbarber/orcli)
[![Go Reference](https://pkg.go.dev/badge/github.com/glenjbarber/orcli.svg)](https://pkg.go.dev/github.com/glenjbarber/orcli)

![Last Commit](https://img.shields.io/github/last-commit/glenjbarber/orcli)
[![Go CI](https://github.com/glenjbarber/orcli/actions/workflows/go-ci.yml/badge.svg?branch=main)](https://github.com/glenjbarber/orcli/actions/workflows/go-ci.yml)

A terminal client for the [OpenRouter.ai](https://openrouter.ai) API.

`orcli` is built around one substantial feature: an interactive, full-screen chat
interface in the manner of Codex, ChatGPT, Claude, and Perplexity. Around it
sits a thin surface of non-interactive commands (`version`, `help`, and the flag
parsing for `--bootstrap` and `--mouse`).

The project is pre-release. `PORTVERSION` is `0.0.0-dev`, and no release has
been scoped.

## Status

The project is at the beginning. The API client is written and tested; the
interface, tools, configuration, persistence, panes and compaction are not.

Implemented:

| Package | Contents |
| --- | --- |
| `internal/openrouter` | HTTP client, SSE stream parser, wire types |

Tested:

| Area | Coverage |
| --- | --- |
| Stream parser | marker required, text kept before a cut, fragments joined on the wire index, counts read in every shape the endpoint uses |
| Request path | the request and its headers, the body reaching the parser unconsumed, a refused request quoted and bounded and redacted, tool calls and usage end to end, cancellation |

There is no `cmd/orcli` yet, so no binary is produced by `make build`.

Not yet written:

| Package | Contents |
| --- | --- |
| `cmd/orcli` | main package: flags, trust gate, wiring |
| `internal/bootstrap` | `--bootstrap` documents and symlink resolution |
| `internal/complete` | Tab completion engine |
| `internal/config` | JSON configuration, colour writer, trusted directories |
| `internal/saved` | SQLite session store, autosave naming |
| `internal/tools` | tool execution: filesystem, git, shell |
| `internal/tui` | the interface: rendering, input, session, panes |

## Design overview

A full account of the design lives in `DESIGN.md`. It is written from the code
and the shape of its history, and it records what the system is and how its
parts fit, where `AGENTS.md` records why individual decisions were taken. The
summary below is a short orientation, not a substitute.

**Where the packages below are not yet in the tree, they are the design as
recorded, not a description of code that exists.**

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

Notion uses a workspace integration token in the `notion.api_key` configuration
member. The model can search and read pages, work with blocks, pages, databases,
data sources, comments, users and file-upload metadata through the public REST
API. `notion_api` exposes the remaining documented JSON operations by name.
Binary file transfer and connector-hosted features such as mail, agent sessions,
and AI memory are not part of the standalone REST client.

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

Accounting is decoded apart from the reply text. The endpoint sends counts in
shapes this client cannot always read, and a figure it cannot parse must not cost
the words beside it: a session with no cost for one response has an inexact
total, which is a thing the ledger can say, while a lost reply is not.

This is the one package that exists.

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

`--config PATH` reads and writes the named file for the current session. It does
not create or rewrite the default file, and a later run without the flag uses the
ordinary search order. The selected file must already exist and be mode `0600`.

The optional `notion` block carries its integration credential and is preserved
as raw JSON so unknown members survive configuration reads. For example:

```json
{"notion":{"api_key":"your-integration-token"}}
```

The optional `github` block carries an `api_key` string and preserves unknown
members in the same way:

```json
{"github":{"api_key":"your-github-token"}}
```

The value is treated as an opaque string. This adds configuration support only;
it does not enable GitHub API requests or other GitHub operations. Keep the
configuration file at mode `0600`.

With `orcli --debug`, the interaction stream is written to
`.orcli-debug.jsonl` in the working directory from startup. During a session,
`/trace` starts capture, `/trace status` reports whether it is active, and
`/trace off` stops it. Runtime capture records from the moment it is enabled.
The enabled state is saved in the active configuration file and restored at the
next startup; an older file without a `trace` member keeps the default, disabled
state.
The stream includes user and assistant messages, slash commands, tool calls and
results, request errors, and stream completion. The file is mode `0600`;
configured provider credentials, secret-bearing fields, known token formats,
and long opaque token-like strings are redacted before each record is written.

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
credential-filtered, because it is shown in a pane and ends up in the terminal
scrollback.

## Build

The `Makefile` is written in the syntax common to BSD make and GNU make, and
repetition is done with a shell loop rather than a make loop so the file parses
the same way under both. Every artifact is written under `build/`, and the
binary is `build/orcli`.

```
make build      build the binary into build/orcli
make test       run the suite
make check      run the suite under the race detector
make lint       gofmt check and go vet
make crossbuild compile and vet every supported target
```

`crossbuild` covers darwin, linux, freebsd, openbsd and netbsd on amd64 and
arm64, with `CGO_ENABLED=0`. DragonFly is absent because the SQLite driver
cannot be built for it, and Windows is absent because the bootstrap loader
compares devices through `syscall.Stat_t`.

The binary is not produced yet, because there is no `cmd/orcli` in the tree. The
build target reports that rather than failing, so the library packages still
build and vet.

## License

See `LICENSE`.
