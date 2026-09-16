package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// exitError never reaches a writer: Run reads its code and prints nothing, which is the point of
// carrying a status through the error channel. Error() exists because the interface wants it.
func TestExitErrorMessage(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "exit status 1", exitError{code: exitBelow}.Error())
	assert.Equal(t, "exit status 2", exitError{code: exitFailed}.Error())
}

// IsBool is what stops kong's scanner taking the next argument as --hide-covered's value, which is
// how the bare form works at all. Asserted here because it is answered inside kong.
func TestOptionalPercentageIsBoolLike(t *testing.T) {
	t.Parallel()

	assert.True(t, optionalPercentage{}.IsBool(), "a bare --hide-covered must not swallow what follows")
}
