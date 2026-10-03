package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/glenjbarber/orcli/internal/config"
)

// version is the build identity.
//
// It is a variable rather than a constant so a build can set it with -ldflags,
// and it is reported by `orcli version` and by every startup report. A build
// that cannot be told apart from another is a build nobody can report a fault
// against.
var version = "0.0.0-dev"

// gate is the trust question, as a variable rather than a call.
//
// This is the seam a test stands in, and it is also what keeps the wiring
// visible in one place rather than spread through run. See ensureTrusted for
// what it does and writeTrust for how an answer is recorded.
var gate = ensureTrusted

// streamsAreTerminal reports whether both streams are terminals.
//
// It is a variable for the same reason gate is: a test needs to decide the answer
// without a terminal, and the real answer comes from a termios read that is
// build-tagged per platform and therefore not reachable from a test run on any
// one of them.
var streamsAreTerminal = func(stdin io.Reader, stdout io.Writer) bool { return false }

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		// The interface is not open at this point, so a failure here has nowhere
		// else to go. The message is on stderr because a reader who ran the
		// program and got a failure has not asked for anything to be written to
		// stdout.
		fmt.Fprintln(os.Stderr, "orcli:", err)
		os.Exit(1)
	}
}

// run is the startup contract, and the order of it is the contract.
//
// stdin, stdout and stderr are parameters rather than the process streams so the
// whole of startup is testable without a terminal. A test that needs a terminal
// to check a flag message is a test that does not get written.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("orcli", flag.ContinueOnError)
	// Output is discarded rather than set to stderr: the flag package would then
	// print the usage and the error, and the caller below prints one message. A
	// bad flag reported twice is a reader looking for the second one.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	var (
		showVersion = fs.Bool("version", false, "print the version and stop")
		showHelp    = fs.Bool("help", false, "print the usage and stop")
		bootstrap   = fs.String("bootstrap", "", "read a document from this path")
		mouse       = fs.Bool("mouse", false, "report the mouse")
		bell        = fs.Bool("bell", false, "ring the terminal bell when a reply arrives")
		color       = fs.Bool("color", false, "write colour")
		dir         = fs.String("dir", "", "run in this directory rather than the current one")
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
	// knowing about before being asked anything, and it is read exactly once so
	// the document that was validated is the one a session is handed.
	if *bootstrap != "" {
		if err := readable(*bootstrap); err != nil {
			return fmt.Errorf("read the bootstrap document: %w", err)
		}
	}

	// A missing configuration file is installed before it is loaded, so the
	// report a first-time reader gets names a file that exists rather than one
	// they have to create themselves.
	if err := config.InstallDefault(); err != nil {
		return err
	}

	cfg, err := config.Load()
	switch {
	case err == nil:
	case errors.Is(err, config.ErrNoAPIKey), errors.Is(err, config.ErrNotFound):
		// Neither absence stops startup. An interface that refuses to open
		// leaves nothing on screen to explain why, and the remedy for both is a
		// key in a file the reader already knows the path of.
		cfg = config.Default()
	default:
		// Everything else is a fault: a bad mode, a malformed body, a directory
		// where the file should be. A file holding a credential that another
		// account can read has already leaked, and that is worth refusing to
		// start over.
		return err
	}

	approval, err := cfg.ApprovalMode()
	if err != nil {
		// The file named a mode this client does not know. Substituting a
		// default would do something other than what the reader wrote, so it is
		// reported by name and startup stops.
		return err
	}

	workDir := *dir
	if workDir == "" {
		if workDir, err = os.Getwd(); err != nil {
			return fmt.Errorf("read the working directory: %w", err)
		}
	}

	s := session{
		Config:     cfg,
		Approval:   approval,
		Bootstrap:  *bootstrap,
		Mouse:      *mouse || cfg.Mouse,
		Bell:       *bell || cfg.Bell,
		Color:      *color || cfg.Color,
		WorkingDir: workDir,
	}

	// A refusal is not a failure. A reader who does not want to approve a
	// directory can still use the interface; it has no tools, since a tool acts
	// on their behalf and this directory is not theirs.
	s.HasTools = gate(workDir, cfg, stdin, stdout, stderr)

	// The check for a terminal is kept even though the interface is not built,
	// because it is the reason a redirected run behaves differently and a reader
	// who redirects this program deserves to be told rather than to be handed a
	// report they did not ask for.
	if !streamsAreTerminal(stdin, stdout) {
		fmt.Fprintln(stderr,
			"orcli: stdin and stdout must both be terminals to open the interface")
	}
	printSession(stdout, s)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout,
		"The interface is not built yet. internal/tui holds the log and nothing else.")
	return nil
}

// session is what a run resolved to, and is reported rather than opened.
//
// It exists so the whole of startup is inspectable without a terminal, which is
// what makes the wiring testable.
type session struct {
	Config     config.Config
	Approval   config.Approval
	HasTools   bool
	Mouse      bool
	Bell       bool
	Color      bool
	Bootstrap  string
	WorkingDir string
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
	fmt.Fprintf(w, "  colour         %s\n", enabled(s.Color))
	fmt.Fprintf(w, "  directory      %s\n", orNone(s.WorkingDir))
	fmt.Fprintf(w, "  bootstrap      %s\n", orNone(s.Bootstrap))
}

// credential reports whether a credential is present, and never what it is.
//
// The figure itself is never written to a diagnostic in this program. Whether it
// is present is the only thing a startup report has any business carrying, since
// every line of it ends up in terminal scrollback.
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
// through the configuration's own named writers, and nothing outside
// internal/config writes bytes to a file holding a credential.
//
// A directory already listed is not asked about again. A refusal returns false
// and records nothing, since an empty line, a key pressed for another reason and
// a non-interactive reader are all the same answer, which is no.
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
// The path is resolved here rather than inside internal/config so a caller can
// see which file is being written, and the edit is delegated so the byte-level
// work stays with the package that owns the file.
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
		// An unknown name is refused by name rather than treated as a prompt,
		// since a script that passed the wrong word has a bug in it that running
		// an interactive session would hide.
		return fmt.Errorf("unknown command %q: the commands are version and help", name)
	}
}

// printUsage writes the usage.
//
// It is written out rather than taken from the flag set, since the flag set
// prints to a discarded writer and a usage generated from the parser would grow a
// flag nobody was meant to be offered.
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
  --color                write colour
  --dir DIR              run in DIR rather than the current directory

configuration:
  A credential is read from the configuration file and from nowhere else.
  OPENROUTER_API_KEY is ignored even when set. The file is found at
  ~/.orcli.json and then at ~/.config/orcli/orcli.json, and the first match wins.
  It must be mode 0600.

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
