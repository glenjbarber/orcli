package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/glenjbarber/orcli/internal/config"
	"github.com/glenjbarber/orcli/internal/keychain"
	"golang.org/x/term"
)

// keychainSet and keychainDelete are keychain.Set/keychain.Delete, held as
// package variables so a test can stand in for the real Keychain the same
// way draw and gate already do elsewhere in this package - this repository's
// own CI runs on Linux, which has no Keychain and no `security` binary at
// all, and a test over the real functions would only ever exercise
// ErrUnsupported there.
var (
	keychainSet    = keychain.Set
	keychainDelete = keychain.Delete
)

// runKeychainCommand implements `orcli keychain <set|list|delete>` (issue
// #67's own "secure way to add/update named API-key items and list their
// names/provider association without displaying secret values" and
// "deletion should remove only the specifically selected item").
//
// It is a command rather than a TUI slash command: every other credential
// surface here (`/attribute`, the cloudflare block) is edited by hand in
// the file or, for a value that is not a secret, through the interactive
// session - but a Keychain secret has to be typed once, with no terminal
// echo, and the interactive session's own input line is not that: it is
// rendered into the transcript and would put the secret on screen the
// moment it was typed. A separate, one-shot command that exits before any
// chat session opens is the only place this can be entered without ever
// being drawn.
func runKeychainCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, cfg config.Config, path string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: orcli keychain <set|list|delete> ...\n%s", keychainUsage)
	}
	switch args[0] {
	case "set":
		return runKeychainSet(ctx, args[1:], stdin, stdout, path)
	case "list":
		return runKeychainList(stdout, cfg)
	case "delete":
		return runKeychainDelete(ctx, args[1:], stdout, cfg, path)
	default:
		return fmt.Errorf("orcli keychain: unknown subcommand %q\n%s", args[0], keychainUsage)
	}
}

const keychainUsage = `usage:
  orcli keychain set NAME ACCOUNT SERVICE   add or update a named Keychain reference, then prompt for the secret
  orcli keychain list                       list configured names, accounts and services - never secret values
  orcli keychain delete NAME                remove a named reference from both the config file and the Keychain`

func runKeychainSet(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, path string) error {
	if len(args) != 3 {
		return fmt.Errorf("orcli keychain set: want NAME ACCOUNT SERVICE, got %d argument(s)", len(args))
	}
	name, account, service := args[0], args[1], args[2]
	ref := keychain.Ref{Account: account, Service: service}

	fmt.Fprintf(stdout, "Secret for %s (%s): ", name, ref)
	secret, err := readSecret(stdin)
	if err != nil {
		return fmt.Errorf("orcli keychain set: reading the secret: %w", err)
	}
	if secret == "" {
		return fmt.Errorf("orcli keychain set: no secret was given")
	}

	if err := keychainSet(ctx, ref, secret); err != nil {
		return err
	}
	if err := config.WriteKeychainItem(path, name, config.KeychainRef{Account: account, Service: service}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\nStored %q (%s). Set \"api_key_keychain\": %q in your configuration file to use it as the credential.\n", name, ref, name)
	return nil
}

// readSecret reads one line from stdin without echoing it, when stdin is an
// interactive terminal - golang.org/x/term.ReadPassword is what actually
// suppresses the echo, and it needs a real terminal file descriptor to do
// that. When stdin is not a terminal (piped input, or a test), it falls
// back to reading one line in the plain way: a pipe has no terminal to
// suppress echo on in the first place, so there is nothing this function
// could additionally protect there, and refusing to work at all with
// piped input would make this command untestable.
func readSecret(stdin io.Reader) (string, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func runKeychainList(stdout io.Writer, cfg config.Config) error {
	if cfg.Keychain == nil || len(cfg.Keychain.Items) == 0 {
		fmt.Fprintln(stdout, "no Keychain items are configured.")
		return nil
	}
	names := make([]string, 0, len(cfg.Keychain.Items))
	for name := range cfg.Keychain.Items {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		ref := cfg.Keychain.Items[name]
		marker := ""
		if name == cfg.APIKeyKeychain {
			marker = " (api_key_keychain)"
		}
		fmt.Fprintf(stdout, "%s: account=%q service=%q%s\n", name, ref.Account, ref.Service, marker)
	}
	return nil
}

func runKeychainDelete(ctx context.Context, args []string, stdout io.Writer, cfg config.Config, path string) error {
	if len(args) != 1 {
		return fmt.Errorf("orcli keychain delete: want exactly NAME, got %d argument(s)", len(args))
	}
	name := args[0]
	if cfg.Keychain == nil {
		return fmt.Errorf("orcli keychain delete: %q is not configured", name)
	}
	ref, ok := cfg.Keychain.Items[name]
	if !ok {
		return fmt.Errorf("orcli keychain delete: %q is not configured", name)
	}

	if err := config.DeleteKeychainItem(path, name); err != nil {
		return err
	}
	if err := keychainDelete(ctx, keychain.Ref{Account: ref.Account, Service: ref.Service}); err != nil {
		// The config reference is already gone at this point. Reported
		// rather than silently ignored, since a Keychain item left
		// behind (locked, or the Delete call itself failed) is the kind
		// of orphaned secret a reader should be told about rather than
		// have an apparently-clean command hide.
		return fmt.Errorf("removed the configuration reference for %q, but removing the Keychain item itself failed: %w", name, err)
	}
	fmt.Fprintf(stdout, "Removed %q.\n", name)
	return nil
}
