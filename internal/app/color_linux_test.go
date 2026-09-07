package app_test

import (
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"

	"github.com/screwyprof/prettycov/internal/app"
)

// Linux ioctls for handing out a pseudo-terminal: unlock the pair, then ask which slave it is.
const (
	tiocsptlck = 0x40045431
	tiocgptn   = 0x80045430
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

	require.NoError(t, master.SetReadDeadline(time.Now().Add(5*time.Second)))

	buf := make([]byte, 4096)
	n, err := master.Read(buf)
	require.NoError(t, err)

	assert.Contains(t, string(buf[:n]), "\x1b[", "a terminal gets the escapes")
	assert.Contains(t, string(buf[:n]), "60.00")
}

// openPTY returns the two ends of a pseudo-terminal. The slave is what a program writes to and is
// a terminal; the master is what a terminal emulator would read.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	require.NoError(t, err)

	t.Cleanup(func() { _ = master.Close() })

	var unlock int

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), tiocsptlck, uintptr(unsafe.Pointer(&unlock)))
	require.Zero(t, errno, "unlock the pty pair")

	var num uint32

	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), tiocgptn, uintptr(unsafe.Pointer(&num)))
	require.Zero(t, errno, "ask which slave")

	slave, err = os.OpenFile("/dev/pts/"+strconv.FormatUint(uint64(num), 10), os.O_RDWR, 0)
	require.NoError(t, err)

	t.Cleanup(func() { _ = slave.Close() })

	require.True(t, term.IsTerminal(int(slave.Fd())), "the slave must be a terminal")

	return master, slave
}
