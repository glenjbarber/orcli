//go:build darwin || linux

package tui

import (
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
)

// The window size is asked for on every paint rather than read once.
//
// The frame is drawn into a fixed region, and a region set to the size the
// terminal had when the program started is a region that is wrong from the first
// resize on: a terminal made shorter clips the log to rows the screen no longer
// has, and one made taller hands back rows the frame never drew into. A reader who
// resizes is not asking for the old size back, and this is the whole of the
// answer.
//
// The signal does not carry the size and does not draw. It raises a flag, and the
// next paint asks the terminal and adopts what it says. That split is the reason
// this works on a terminal that reports its size through some other mechanism as
// well as on one that raises SIGWINCH: the paint asks regardless, and the flag
// only says there is something worth asking about sooner than the next keypress.

// resizeFlag is set when the terminal reports it changed size.
//
// It is atomic rather than a plain bool because the signal handler runs on its
// own goroutine and the paint reads it on this one, and a bool written by one and
// read by the other is a race the detector reports and a reader sees as a frame
// drawn to a size nobody chose.
var resizeFlag atomic.Bool

// watchWindow raises the flag when the terminal changes size.
//
// It returns a function that stops watching, so every path out of the loop
// releases the handler rather than leaving it pointing at a loop that has
// returned. Clearing the flag on stop is what makes a signal arriving after the
// loop has gone harmless rather than a write to a frame nobody is reading.
func watchWindow() func() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, windowChange)

	done := make(chan struct{})
	stopped := false

	go func() {
		for {
			select {
			case <-done:
				return
			case <-sig:
				resizeFlag.Store(true)
			}
		}
	}()

	return func() {
		if stopped {
			return
		}
		stopped = true
		signal.Stop(sig)
		close(done)
		resizeFlag.Store(false)
	}
}

// resized consumes the flag, and reports whether a resize was seen.
//
// It consumes rather than reports so a caller cannot ask twice and be told yes
// twice: the second paint after a resize asks the terminal, finds the same size,
// and adopts it, which is a no-op rather than a second redraw of the log.
func resized() bool { return resizeFlag.Swap(false) }

// windowChange is the signal a terminal raises when it is resized.
var windowChange = syscall.SIGWINCH
