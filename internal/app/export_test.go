package app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// ClearColorEnv puts the environment in the state where only the destination decides whether to
// colour. In package app so the colour tests inside it and the app_test suite outside share one
// definition: a variable that joins the convention is then added once, rather than to whichever
// copy the author happened to be reading.
//
// Its callers cannot be parallel: t.Setenv panics under t.Parallel.
func ClearColorEnv(t *testing.T) {
	t.Helper()

	// Registers the restore, then clears it: NO_COLOR set to anything, empty included, means no.
	t.Setenv("NO_COLOR", "")
	require.NoError(t, os.Unsetenv("NO_COLOR"))
	t.Setenv("TERM", "xterm")
}
