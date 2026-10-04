//go:build darwin || linux

package tui

import (
	"os"
	"syscall"
)

// interruptSignals returns the signals that end the process, so the handler can
// put the terminal back before they do.
//
// SIGHUP is here because it is what a terminal sends when the window closes, and
// that is exactly the moment a reader's shell is about to be handed back a
// terminal this program left in raw mode. SIGQUIT is here on the same terms. What
// is not here is any signal a program might want to handle for itself, since the
// job of this handler is the terminal and not the program's policy.
func interruptSignals() []os.Signal {
	return []os.Signal{
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGHUP,
		syscall.SIGQUIT,
	}
}
