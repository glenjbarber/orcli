package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/openrouter"
	"github.com/glenjbarber/orcli/internal/tui"
)

// version is the build identity.
//
// It is a variable rather than a constant so a build can set it with -ldflags, and
// it is reported by `orcli version` and by every startup report. A build that cannot
// be told apart from another is a build nobody can report a fault against.
var version = "0.0.0-dev"

// gate is the trust question, as a variable rather than a call.
//
// This is the seam a test stands in, and it is also what keeps the wiring visible
// in one place rather than spread through run. See ensureTrusted for what it does
// and writeTrust for how an answer is recorded.
var gate = ensureTrusted

// draw is the interface being opened, as a variable rather than a call.
//
// It is the same seam as gate, for the same reason and with the same cost: opening
// the interface is a call into a terminal, and a test that cannot stand in for it is
// a test that can only run on a machine with a terminal attached. Naming the call
// here keeps the wiring visible and lets a test run the whole of startup without
// one.
//
// See openInterface for how the terminal is handed to tview.
var draw = openInterface

// newTransport is the API client, as a variable rather than a call.
//
// It is a third seam for the same reason as the two above, and it is the one that
// keeps a test from reaching the network: a test that stands in for the client can
// exercise a whole turn without a credential and without a request.
var newTransport = openrouter.New

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		// The interface is not open at this point, so a failure here has nowhere
		// else to go. The message is on stderr because a reader who ran the program
		// and got a failure has not asked for anything to be written to stdout.
		fmt.Fprintln(os.Stderr, "orcli:", err)
		os.Exit(1)
	}
}

// run is the startup contract, and the order of it is the contract.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("orcli", flag.ContinueOnError)
	// Output is discarded rather than set to stderr: the flag package would then
	// print the usage and the error, and the caller below prints one message. A bad
	// flag reported twice is a reader looking for the second one.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	var (
		showVersion     = fs.Bool("version", false, "print the version and stop")
		showHelp        = fs.Bool("help", false, "print the usage and stop")
		bootstrap       = fs.String("bootstrap", "", "read a document from this path")
		mouse           = fs.Bool("mouse", false, "report the mouse")
		bell            = fs.Bool("bell", false, "ring the terminal bell when a reply arrives")
		breakInterval   = fs.Int("break-interval", 0, "minutes before a screen-break reminder (0 uses the configured default)")
		breakBell       = fs.Bool("break-bell", false, "ring the terminal bell when a screen break starts")
		color           = fs.Bool("color", false, "write colour")
		paneActiveColor = fs.String("pane-active-color", "", "#rrggbb for a pane with a running worker (default "+config.DefaultPaneActiveColor+")")
		paneDoneColor   = fs.String("pane-done-color", "", "#rrggbb for a pane whose worker just finished (default "+config.DefaultPaneDoneColor+")")
		dir             = fs.String("dir", "", "run in this directory rather than the current directory")
	)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return nil
		}
		return err
	}

	switch {
	case *showVersion:
		fmt.Fprintf(stdout, "orcli %s\n", version)
		return nil
	case *showHelp:
		printUsage(stdout)
		return nil
	}

	if rest := fs.Args(); len(rest) > 0 {
		return subcommand(rest[0], rest[1:], stdout)
	}

	// The bootstrap document is named by the reader and cannot be read. It is
	// reported before the configuration, since a document that is wrong is worth
	// knowing about before being asked anything, and it is read exactly once so the
	// document that was validated is the one a session is handed.
	if *bootstrap != "" {
		if err := readable(*bootstrap); err != nil {
			return fmt.Errorf("read the bootstrap document: %w", err)
		}
	}

	// A missing configuration file is installed before it is loaded, so the report
	// a first-time reader gets names a file that exists rather than one they have to
	// create themselves.
	if err := config.InstallDefault(); err != nil {
		return err
	}

	cfg, err := config.Load()
	switch {
	case err == nil:
	case errors.Is(err, config.ErrNoAPIKey), errors.Is(err, config.ErrNotFound):
		// Neither absence stops startup. An interface that refuses to open leaves
		// nothing on screen to explain why, and the remedy for both is a key in a
		// file the reader already knows the path of.
		cfg = config.Default()
	default:
		// Everything else is a fault: a bad mode, a malformed body, a directory
		// where the file should be. A file holding a credential that another account
		// can read has already leaked, and that is worth refusing to start over.
		return err
	}

	approval, err := cfg.ApprovalMode()
	if err != nil {
		// The file named a mode this client does not know. Substituting a default
		// would do something other than what the reader wrote, so it is reported by
		// name and startup stops.
		return err
	}

	// A negative figure has no reading as an interval, on the same grounds as
	// the configuration file's own break_interval_minutes. Zero is not refused
	// here: it means the flag was not given, and the configured figure, itself
	// validated at Load, stands instead.
	if *breakInterval < 0 {
		return fmt.Errorf("--break-interval must be positive")
	}

	// A flag's colour is checked here, on the same grounds the configuration
	// file's own PaneActiveColor/PaneDoneColor are checked at Load: a reader
	// who mistyped one is told which flag and what shape it wanted, rather
	// than finding the pane bar quietly uncoloured.
	if err := checkPaneColorFlag("--pane-active-color", *paneActiveColor); err != nil {
		return err
	}
	if err := checkPaneColorFlag("--pane-done-color", *paneDoneColor); err != nil {
		return err
	}
	breakIntervalMinutes := cfg.BreakIntervalMinutes
	if *breakInterval > 0 {
		breakIntervalMinutes = *breakInterval
	}

	workDir := *dir
	if workDir == "" {
		if workDir, err = os.Getwd(); err != nil {
			return fmt.Errorf("read the working directory: %w", err)
		}
	}

	s := session{
		Config:               cfg,
		Approval:             approval,
		Bootstrap:            *bootstrap,
		Mouse:                *mouse || cfg.Mouse,
		Bell:                 *bell || cfg.Bell,
		BreakIntervalMinutes: breakIntervalMinutes,
		BreakBell:            *breakBell || cfg.BreakBell,
		Color:                *color || cfg.Color,
		PaneActiveColor:      firstNonEmpty(*paneActiveColor, cfg.PaneActiveColor),
		PaneDoneColor:        firstNonEmpty(*paneDoneColor, cfg.PaneDoneColor),
		WorkingDir:           workDir,
	}

	// A refusal is not a failure. A reader who does not want to approve a directory
	// can still use the interface; it has no tools, since a tool acts on their
	// behalf and this directory is not theirs.
	s.HasTools = gate(workDir, cfg, stdin, stdout, stderr)

	// The interface is opened here, between the trust question and the report, so the
	// directory the reader just answered about is the one the session carries.
	//
	if err := draw(ctx, s.tuiSession(), s.Config, stdin, stdout); err != nil {
		if !errors.Is(err, tui.ErrNoTerminal) {
			return err
		}
		fmt.Fprintf(stderr, "orcli: %v\n", err)
		printSession(stdout, s)
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout,
			"The interface needs a terminal. Nothing reads keys and no turn is sent.")
		return nil
	}

	printSession(stdout, s)
	return nil
}

// session is what a run resolved to, and is reported rather than opened.
//
// It exists so the whole of startup is inspectable without a terminal, which is
// what makes the wiring testable.
type session struct {
	Config               config.Config
	Approval             config.Approval
	HasTools             bool
	Mouse                bool
	Bell                 bool
	BreakIntervalMinutes int
	BreakBell            bool
	Color                bool
	PaneActiveColor      string
	PaneDoneColor        string
	Bootstrap            string
	WorkingDir           string
}

// tuiSession builds the interface's own session from what startup resolved.
//
// The translation is here rather than in internal/tui because the interface does
// not read a configuration file: it takes an Options value, and something has to
// decide what the answers to the startup questions become. Keeping it in main is
// what lets the interface take a value instead of reaching back for a file that
// holds a credential.
//
// The approval mode is converted rather than cast. They are two types that spell the
// same three modes, and a cast would let a future fourth through as an empty string
// the bars would print as a blank field.
//
// Cognito is not carried across, since the marker is a file beside the configuration
// rather than a member of it, and there is no field on Config to copy from. Nothing
// reads the marker yet, so the mode arrives as off rather than as a decision the
// reader did not make.
//
// The Cloudflare key is not carried across either, and that is the rule this
// dispatcher exists to keep. A command in main reads the block itself, so a second
// provider credential does not sit in the interface for the length of a session doing
// nothing with it.
func (s session) tuiSession() *tui.Session {
	opts := tui.Options{
		APIKey:        s.Config.APIKey,
		Model:         s.Config.Model,
		Provider:      s.Config.Provider,
		Approval:      tuiApproval(s.Approval),
		WorkingDir:    s.WorkingDir,
		Color:         s.Color,
		Bell:          s.Bell,
		BreakInterval: time.Duration(s.BreakIntervalMinutes) * time.Minute,
		BreakBell:     s.BreakBell,
		Mouse:         s.Mouse,
	}

	// Each colour is parsed only if it is present and well-formed, which run
	// has already checked for the flag and Load has already checked for the
	// file. A colour that somehow still fails to parse is left nil rather than
	// substituted for, which is the same "a pane with no opinion draws in
	// chrome" fallback a session built with neither field set gets.
	if rgb, ok := tui.RGBFromHex(s.PaneActiveColor); ok {
		opts.PaneActiveColor = &rgb
	}
	if rgb, ok := tui.RGBFromHex(s.PaneDoneColor); ok {
		opts.PaneDoneColor = &rgb
	}
	return tui.New(opts)
}

// firstNonEmpty returns a if it is not empty, and b otherwise.
//
// It is how a flag overrides the configuration file for a string setting: a
// flag the reader did not pass parses to its zero value, which is the one
// value that has to mean "nothing was said" rather than "the empty string was
// chosen", since every setting this file reads named this way is a thing a
// reader opts into rather than explicitly turns off with an empty value.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// checkPaneColorFlag refuses a flag value that is present but not `#rrggbb`,
// naming the flag so a reader who mistyped one is told which.
func checkPaneColorFlag(flag, value string) error {
	if value == "" {
		return nil
	}
	if _, ok := tui.RGBFromHex(value); !ok {
		return fmt.Errorf("%s must be #rrggbb", flag)
	}
	return nil
}

// tuiApproval converts one approval mode to the other.
//
// The mapping is total over the three modes and empty over anything else, so a mode
// this build has not heard of arrives at the interface as ask rather than as a blank
// field. Ask is the mode that still asks, which is the right thing to fall back to
// when this program does not know what it was told.
func tuiApproval(mode config.Approval) tui.Approval {
	switch mode {
	case config.ApprovalAllow:
		return tui.ApprovalAllow
	case config.ApprovalDeny:
		return tui.ApprovalDeny
	default:
		return tui.ApprovalAsk
	}
}

// openInterface runs the interface over a terminal until the reader leaves.
//
// tcell owns terminal setup, input, output, and screen size for this run.
//
// The dispatcher is built here rather than held on the session, since a command
// needs the configuration and the held proposal and the interface needs neither. The
// transport is built on the same seam, so a reader with no credential gets an
// interface that opens and tells them so when they type a question, rather than one
// that refused to start.
func openInterface(ctx context.Context, s *tui.Session, cfg config.Config,
	stdin io.Reader, stdout io.Writer) error {

	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	if !inOK || !outOK || in != os.Stdin || out != os.Stdout {
		return tui.ErrNoTerminal
	}

	d := newDispatcherFor(cfg)
	d.canAsk = func() bool { return canAsk(s) }

	// The dispatcher needs the session so `/model` can change the model the next turn
	// is sent with, not only the one written to the file.
	d.withSession(s)

	// The confirmation is wired here rather than in ask, since the writer belongs to
	// main and the interface holds no configuration. A turn that came back with text
	// is what writes the model, and that is the connection there is no command for.
	return tui.Start(ctx, s,
		d.Run,
		ask(s, newTransport(cfg.APIKey), cfg.AttributionID, confirmModel(s), d.cloudflareReady),
	)
}

// printSession reports a session as plain text.
func printSession(w io.Writer, s session) {
	fmt.Fprintf(w, "orcli %s\n", version)
	fmt.Fprintf(w, "  configuration  %s\n", orNone(config.Path()))
	fmt.Fprintf(w, "  credential     %s\n", credential(s.Config.APIKey))
	fmt.Fprintf(w, "  provider       %s\n", orNone(s.Config.Provider))
	fmt.Fprintf(w, "  model          %s\n", orNone(s.Config.Model))
	fmt.Fprintf(w, "  approval       %s\n", s.Approval)
	fmt.Fprintf(w, "  tools          %s\n", enabled(s.HasTools))
	fmt.Fprintf(w, "  mouse          %s\n", enabled(s.Mouse))
	fmt.Fprintf(w, "  bell           %s\n", enabled(s.Bell))
	fmt.Fprintf(w, "  break          every %dm, bell %s\n", s.BreakIntervalMinutes, enabled(s.BreakBell))
	fmt.Fprintf(w, "  colour         %s\n", enabled(s.Color))
	fmt.Fprintf(w, "  pane active    %s\n", orNone(s.PaneActiveColor))
	fmt.Fprintf(w, "  pane done      %s\n", orNone(s.PaneDoneColor))
	fmt.Fprintf(w, "  directory      %s\n", orNone(s.WorkingDir))
	fmt.Fprintf(w, "  bootstrap      %s\n", orNone(s.Bootstrap))
}

// credential reports whether a credential is present, and never what it is.
//
// The figure itself is never written to a diagnostic in this program. Whether it is
// present is the only thing a startup report has any business carrying, since every
// line of it ends up in terminal scrollback.
func credential(key string) string {
	switch {
	case key == "":
		return "absent"
	default:
		return "present"
	}
}

// ensureTrusted asks about a directory and records the answer.
//
// It is a thin wrapper rather than a direct call, because the wiring is what this
// file is for: the reader and the writer are supplied here so the record goes
// through the configuration's own named writers, and nothing outside internal/config
// writes bytes to a file holding a credential.
//
// A directory already listed is not asked about again. A refusal returns false and
// records nothing, since an empty line, a key pressed for another reason and a
// non-interactive reader are all the same answer, which is no.
func ensureTrusted(dir string, cfg config.Config, stdin io.Reader, stdout, stderr io.Writer) bool {
	t := config.Trust{
		In:    stdin,
		Out:   stdout,
		Warn:  func(err error) { fmt.Fprintln(stderr, "orcli:", err) },
		Read:  func() (config.Config, error) { return cfg, nil },
		Write: func(updated config.Config) error { return writeTrust(updated) },
	}
	return t.EnsureTrusted(dir)
}

// writeTrust records the trusted directories in the configuration file.
//
// The path is resolved here rather than inside internal/config so a caller can see
// which file is being written, and the edit is delegated so the byte-level work
// stays with the package that owns the file.
func writeTrust(cfg config.Config) error {
	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	if err := config.AddTrusted(path, cfg.Trusted); err != nil {
		return fmt.Errorf("record the trusted directory in %s: %w", path, err)
	}
	return nil
}

// readable reports whether a named file can be opened and is not a directory.
func readable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a document", path)
	}
	return nil
}

// subcommand runs a non-interactive command.
//
// There are two, and both print and stop. They exist so a script can ask what this
// build is without starting a session that will not finish.
func subcommand(name string, _ []string, stdout io.Writer) error {
	switch name {
	case "version":
		fmt.Fprintf(stdout, "orcli %s\n", version)
		return nil
	case "help":
		printUsage(stdout)
		return nil
	default:
		// An unknown name is refused by name rather than treated as a prompt, since
		// a script that passed the wrong word has a bug in it that running an
		// interactive session would hide.
		return fmt.Errorf("unknown command %q: the commands are version and help", name)
	}
}

// printUsage writes the usage.
//
// It is written out rather than taken from the flag set, since the flag set prints
// to a discarded writer and a usage generated from the parser would grow a flag
// nobody was meant to be offered.
func printUsage(w io.Writer) {
	fmt.Fprint(w, `orcli is a terminal client for the OpenRouter API.

usage:
  orcli                  open the interface
  orcli version          print the version
  orcli help             print this message

flags:
  --version              print the version and stop
  --help                 print this message and stop
  --bootstrap PATH       read a document from PATH
  --mouse                report the mouse
  --bell                 ring the terminal bell when a reply arrives
  --break-interval MIN   minutes before a screen-break reminder (default 22)
  --break-bell           ring the terminal bell when a screen break starts
  --color                write colour
  --pane-active-color #rrggbb   colour for a pane with a running worker
  --pane-done-color #rrggbb     colour for a pane whose worker just finished
  --dir DIR              run in DIR rather than the current directory

configuration:
  A credential is read from the configuration file and from nowhere else.
  OPENROUTER_API_KEY is ignored even when set. The file is found at
  ~/.orcli.json and then at ~/.config/orcli/orcli.json, and the first match wins.
  It must be mode 0600.

in the interface:
  Type a question and press enter. There is no /connect: your first question is
  what proves the connection, and the model you are answered by is written to the
  configuration file only once the endpoint has answered. Type /model NAME to
  choose one and /model last to go back to the one before it. Type /cloudflare
  to manage DNS records, and /cloudflare confirm to apply a change it showed you.
  /quit leaves.

tools:
  A directory is asked about once and the answer is recorded. A reader who
  declines gets the interface without tools rather than a refusal to start.
`)
}

// orNone renders an empty string as a dash.
//
// A field with no value is shown as a dash rather than hidden, so the report does
// not shift shape as values arrive.
func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// enabled renders a boolean as a word, so a report reads as English.
func enabled(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
