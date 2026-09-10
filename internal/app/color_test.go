// Internal, unlike the rest: it reaches the isTerminal seam and the colorMode values, which are
// not part of what the package offers.
//
//nolint:testpackage // see above.
package app

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"

	"github.com/screwyprof/prettycov"
)

// Every branch of palette, on every platform. isTerminal is answered here rather than by a pty, so
// the guards and the order they run in are pinned even where no pty can be had; the one branch
// that needs a real terminal to mean anything has its own test in color_linux_test.go.
//
//nolint:paralleltest // t.Setenv and the isTerminal swap are both process-wide.
func TestPalette(t *testing.T) {
	ClearColorEnv(t)

	// The heuristic itself, before it is stubbed: /dev/null is a character device, which is what
	// the old Stat-based check mistook for a terminal and coloured.
	t.Run("auto asks the descriptor, not the device type", func(t *testing.T) {
		null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		require.NoError(t, err)

		t.Cleanup(func() { _ = null.Close() })

		assert.Equal(t, prettycov.Plain, colorAuto.palette(null))
	})

	isTerminal = func(int) bool { return true }

	t.Cleanup(func() { isTerminal = term.IsTerminal })

	tests := []struct {
		name string
		mode colorMode
		key  string
		val  string
		want prettycov.Palette
	}{
		{name: "auto to a terminal", mode: colorAuto, want: prettycov.ANSI},
		{name: "auto with NO_COLOR", mode: colorAuto, key: "NO_COLOR", val: "1", want: prettycov.Plain},
		{name: "auto with NO_COLOR empty", mode: colorAuto, key: "NO_COLOR", val: "", want: prettycov.Plain},
		{name: "auto on a dumb terminal", mode: colorAuto, key: "TERM", val: "dumb", want: prettycov.Plain},
		{name: "never to a terminal", mode: colorNever, want: prettycov.Plain},
		// The mode is settled before the environment is consulted, and nothing else pins that
		// order: move the NO_COLOR lookup above the switch and only this fails.
		{name: "always ignores NO_COLOR", mode: colorAlways, key: "NO_COLOR", val: "1", want: prettycov.ANSI},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.key != "" {
				t.Setenv(tc.key, tc.val)
			}

			// Any *os.File will do: isTerminal above says yes for it without looking.
			assert.Equal(t, tc.want, tc.mode.palette(os.Stderr))
		})
	}

	t.Run("auto to something that is not a file", func(t *testing.T) {
		assert.Equal(t, prettycov.Plain, colorAuto.palette(&bytes.Buffer{}))
	})
}
