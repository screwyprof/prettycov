package app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// ClearColorEnv puts the environment in the state where only the destination decides whether to
// colour. In package app so both test packages share one definition. Callers cannot be parallel:
// t.Setenv panics under t.Parallel.
func ClearColorEnv(t *testing.T) {
	t.Helper()

	// Registers the restore, then clears it: NO_COLOR set to anything, empty included, means no.
	t.Setenv("NO_COLOR", "")
	require.NoError(t, os.Unsetenv("NO_COLOR"))
	t.Setenv("TERM", "xterm")
}
