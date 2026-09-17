package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// exitError is unexported, so this is the one thing about it no external test can reach: its
// message. Nothing prints it today. Status checks ExitCodeOf first, and an error carrying a code
// has already been reported, so Error exists to satisfy the interface and is the fallback for the
// day something formats one anyway.
//
// Pinned rather than left to that day: "exit status 1" is what a Go reader expects from a value
// standing in for a process status, and an error whose message is empty or a struct dump would be
// the kind of thing nobody notices until it is in a log.
func TestExitErrorSaysTheStatusItCarries(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "exit status 1", exitError{code: ExitBelow}.Error())
	assert.Equal(t, "exit status 2", exitError{code: ExitFailed}.Error())
}
