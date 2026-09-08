package app_test

import (
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

// The yes branch of the auto heuristic. Every other colour test proves a negative — a buffer, a
// regular file, NO_COLOR — because the branch that says yes needs a real terminal, and a pty is
// the only way to have one in a test.
//
// Linux only: macOS hands out pseudo-terminals through different ioctls, and CI is ubuntu.
//
//nolint:paralleltest // t.Setenv cannot be combined with t.Parallel.
func TestRunAutoColorToATerminal(t *testing.T) {
	// Registers the restore, then clears it: NO_COLOR set to anything, empty included, means no.
	t.Setenv("NO_COLOR", "")
	require.NoError(t, os.Unsetenv("NO_COLOR"))
	t.Setenv("TERM", "xterm")

	master, slave := openPTY(t)

	// No -color at all, so the default really is auto.
	assert.Equal(t, codeOK, app.Run([]string{writeProfile(t, profile)}, slave, os.Stderr))
	require.NoError(t, slave.Close())

	// A ceiling in case the write never happens; on the way through it returns at once.
	require.NoError(t, master.SetReadDeadline(time.Now().Add(5*time.Second)))

	buf := make([]byte, 4096)
	n, err := master.Read(buf)
	require.NoError(t, err)

	assert.Contains(t, string(buf[:n]), "\x1b[", "a terminal gets the escapes")
}

// openPTY returns the two ends of a pseudo-terminal. The slave is what a program writes to and is
// a terminal; the master is what a terminal emulator would read.
//
// Reading the master is why both ends are needed. Writing to the master would come back as echo,
// which ECHOCTL rewrites: each ESC returns as the two bytes "^[", so the escapes could not be
// matched. Slave to master is the output direction and arrives untouched.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	require.NoError(t, err)

	t.Cleanup(func() { _ = master.Close() })

	require.NoError(t, unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0), "unlock the pair")

	num, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	require.NoError(t, err, "ask which slave")

	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(num), os.O_RDWR, 0)
	require.NoError(t, err)

	t.Cleanup(func() { _ = slave.Close() })

	require.True(t, term.IsTerminal(int(slave.Fd())), "the slave must be a terminal")

	return master, slave
}
