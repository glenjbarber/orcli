# Startup

The order `run` performs is the contract, and the streams are parameters so the
whole of it is testable without a terminal.

Code: `cmd/orcli/main.go` in full.

The decision that removed the connection command is 0003 in
`staged/adr-0003-first-request-is-the-probe.txt`, and the section on the
confirmation below is what replaced it.

## The order

```go
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error
```

1. parse the flags
2. `--version` or `--help`, and stop
3. a subcommand, and stop
4. read the bootstrap document if one was named
5. install the default configuration file if there is none
6. load the configuration
7. settle the approval mode
8. resolve the working directory
9. ask about that directory
10. open the interface, if the streams are terminals
11. report

Each step refuses rather than substituting, with one exception that is named below.
A step that guessed would do something other than what the reader asked for, and
the report at the end is what tells them what happened.

## The flags

| Flag | Default | What it does |
| --- | --- | --- |
| `--version` | | print the version and stop |
| `--help` | | print the usage and stop |
| `--bootstrap PATH` | | read a document from PATH |
| `--mouse` | false | report the mouse |
| `--bell` | false | ring the bell when a reply arrives |
| `--color` | false | write colour |
| `--dir DIR` | the working directory | run in DIR |

The flag set's output is discarded and the usage is written out by hand in
`printUsage`, since a usage generated from the parser would grow a flag nobody was
meant to be offered.

## The three absences that do not stop startup

```go
switch {
case err == nil:
case errors.Is(err, config.ErrNoAPIKey), errors.Is(err, config.ErrNotFound):
    cfg = config.Default()
default:
    return err
}
```

A missing file and a missing credential both continue. The comment records why: an
interface that refuses to open leaves nothing on screen to explain why, and the
remedy for both is a key in a file the reader already knows the path of.

Everything else stops. A file holding a credential that another account can read
has already leaked, and that is worth refusing to start over.

## The approval mode

```go
approval, err := cfg.ApprovalMode()
if err != nil { return err }
```

The file named a mode this client does not know, and startup stops rather than
substituting a default, since a default would do something other than what the
reader wrote.

The mode is converted rather than cast when it crosses into the interface.
`tuiApproval` is total over the three modes and falls through to ask, so a fourth
mode this build has not heard of arrives at the interface still asking rather than
as a blank field.

## The trust gate

```go
s.HasTools = gate(workDir, cfg, stdin, stdout, stderr)
```

A refusal is not a failure. A reader who does not want to approve a directory
still gets the interface; it has no tools, since a tool acts on their behalf and
that directory is not theirs.

The gate is a package variable rather than a call, and `gate`, `draw`,
`tuiStreamsAreTerminal` and `newTransport` are all variables for the same reason:
each is a seam a test stands in, and naming them keeps the wiring of startup
visible in one file rather than spread through it.

`ensureTrusted` builds a `config.Trust` with the reader and writer supplied, so
the record goes through the configuration's own named writers. Nothing outside
`internal/config` writes bytes to a file holding a credential.

## Opening the interface

```go
if tuiStreamsAreTerminal(stdin, stdout) {
    if err := draw(ctx, s.tuiSession(), s.Config, stdin, stdout); err != nil {
        return err
    }
    printSession(stdout, s)
    return nil
}
```

**The terminal check is a termios read**, not a stat. `StdoutIsATerminal` is
`IsTerminal(os.Stdout.Fd())`, and a stat cannot tell a terminal from a character
device that is not one. The comment on the variable says why it is a second seam
rather than a folding of `draw`: a test standing in for both would not be testing
that the interface opens when the reader is at a terminal, since the whole
condition would be the stand-in.

**A redirected run is told why rather than drawn on.** The interface writes escape
sequences, and a reader who piped this on purpose would get noise rather than a
transcript. The message goes to stderr and says plainly that nothing reads keys
and no turn is sent.

**The size is read in main, not inside the interface.** The descriptor is the
caller's, so main is the program that can name it. A terminal reporting no size is
refused rather than drawn into, since every row would be cut to no width.

## What crosses into the interface

```go
func (s session) tuiSession() *tui.Session {
    opts := tui.Options{
        APIKey:     s.Config.APIKey,
        Model:      s.Config.Model,
        Provider:   s.Config.Provider,
        Approval:   tuiApproval(s.Approval),
        WorkingDir: s.WorkingDir,
        Color:      s.Color,
        Bell:       s.Bell,
        Mouse:      s.Mouse,
    }
    return tui.New(opts)
}
```

The translation lives in main rather than in `internal/tui` so the interface takes
a value rather than reaching back for a file holding a credential. The interface
does not read a configuration file at all.

**Two things are not carried across, and the comment names both.**

*Cognito* is not carried, since the marker is a file beside the configuration
rather than a member of it and there is no field on `Config` to copy from. The
mode arrives as off rather than as a decision the reader did not make.

*The Cloudflare key* is not carried. A command in main reads the block itself, so a
second provider credential does not sit in the interface for the length of a
session doing nothing with it.

## The connection, which is the reader's first question

There is no connection command. The wiring in `openInterface` is where the probe
lives:

```go
return tui.Start(ctx, s, tui.NewScreen(out, size),
    d.Run,
    ask(s, newTransport(cfg.APIKey), cfg.AttributionID, confirmModel(s)),
)
```

A reader's own first question is the probe. A turn that came back with text has
proved the credential and the model together, and `confirmModel` is what writes
the model to the file on that evidence.

**A turn that delivered nothing confirms nothing**, even when the endpoint called
it finished, since a cut stream has proved nothing and a model written on that
evidence is one nobody has reached the endpoint with.

**It writes once per session**, since the reader's first question is the probe and
every turn after that writing the same model again is a write nobody asked for.

**A failed write is a row in the log rather than a refusal.** The turn was answered
and the answer is the thing the reader asked for, so a model that could not be
written is a nuisance rather than a lost reply.

**A confirmation does not record the model it replaced.** See 0004: a reader's
first question is a probe rather than a move.

Three seams carry the write, all in `cmd/orcli/confirm.go` and all substituted by
every test: `configPath`, `writeModel` and `writeModelSwap`. Without them a test
proving a model was written would read the reader's own configuration file.

## The dispatcher

```go
func newDispatcherFor(cfg config.Config) *dispatcher
d.withSession(s)
```

Built here rather than held on the session, since a command needs the
configuration and the held proposal and the interface needs neither. The transport
is built on the same seam, so a reader with no credential gets an interface that
opens and tells them so when they type a question, rather than one that refused to
start.

`withSession` is what lets `/model` change the model the next turn is sent with,
not only the one written to the file. A dispatcher with no session can still run
the commands that do not touch one, which is what a test over `/cloudflare` does.

## The report

`printSession` writes plain text, one line per field, and the credential line
carries `present` or `absent` and never the value:

```go
func credential(key string) string {
    switch {
    case key == "":
        return "absent"
    default:
        return "present"
    }
}
```

Every line of the report ends up in terminal scrollback, so whether a credential is
present is the only thing it has any business carrying.

## Known gaps

1. **`--bootstrap` is validated and not used.** `readable` opens the file and
   refuses a directory, and the path is handed to `session.Bootstrap` for the
   report, but nothing reads the document's contents.

2. **`cognito` never arrives true.** `Options.Cognito` exists and `Spawn` refuses
   under it, but startup has no path that sets it.

3. **`hasTools` is not carried into the interface either.** `session.HasTools` is
   reported and gates nothing: `tui.Options` has no field for it, and no tool is
   offered.

4. **The model is not chosen from a catalogue.** `ModelIsOffered` is exported and
   has no caller, since the transport has no models endpoint, so a reader naming a
   model the endpoint does not offer finds out from a refusal rather than before
   the request.