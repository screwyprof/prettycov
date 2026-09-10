package app_test

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/screwyprof/prettycov/internal/app"
)

// Linux only: macOS hands out pseudo-terminals through different ioctls, and CI is ubuntu.

// The yes branch of the auto heuristic against a real terminal. The guards around it are pinned
// on every platform in color_test.go through the isTerminal seam; this is the one check that the
// seam's real implementation says yes to a terminal, and a pty is the only way to have one.
//
//nolint:paralleltest // t.Setenv cannot be combined with t.Parallel.
func TestRunAutoColorToATerminal(t *testing.T) {
	app.ClearColorEnv(t)

	assert.Contains(t, runToTerminal(t), "\x1b[", "a terminal gets the escapes")
}

// runToTerminal renders the report to a real terminal and returns what the terminal received.
func runToTerminal(t *testing.T) string {
	t.Helper()

	master, slave := openPTY(t)

	// Drained from the start, not after: app.Run writes the whole report into the slave, and with
	// nobody reading the master a report past the line discipline's buffer would block forever —
	// a package timeout rather than a failed assertion.
	var out bytes.Buffer

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = io.Copy(&out, master)
	}()

	require.Equal(t, codeOK, app.Run([]string{writeProfile(t, profile)}, slave, os.Stderr))

	// Closing the last slave makes the master's read fail, which is what ends the copy.
	require.NoError(t, slave.Close())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out reading the terminal")
	}

	return out.String()
}

// openPTY returns the two ends of a pseudo-terminal. The slave is what a program writes to and is
// a terminal; the master is what a terminal emulator would read.
//
// Reading the master is why both ends are needed. Writing to the master would come back as echo,
// which ECHOCTL rewrites: each ESC returns as the two bytes "^[", so the escapes could not be
// matched. Slave to master is the output direction and arrives untouched.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()

	// O_NOCTTY on both ends. Without it, a test binary that is a session leader with no controlling
	// terminal — which is how some sandboxes start one — would adopt this pty as its controlling
	// terminal, and closing the master below would SIGHUP the process group and kill the run with
	// no failure to read.
	//
	// Every failure to get the pair is a skip, not a failure: a container without devpts has no
	// pty to give, and a kernel before 4.7 can pair /dev/ptmx with a different devpts instance
	// than /dev/pts, so the slave's number names a file that is missing or someone else's.
	// Skipping says the branch went untested, where failing would blame this repository for
	// the sandbox.
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}

	t.Cleanup(func() { _ = master.Close() })

	if err = unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("cannot unlock the pseudo-terminal: %v", err)
	}

	num, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("cannot ask which slave: %v", err)
	}

	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(num), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open the slave: %v", err)
	}

	t.Cleanup(func() { _ = slave.Close() })

	require.True(t, term.IsTerminal(int(slave.Fd())), "the slave must be a terminal")

	return master, slave
}
