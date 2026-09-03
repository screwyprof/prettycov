package prettycov

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Internal, because the glyphs are how this package draws a tree, not something a caller picks.
// Drawing a report is not worth a panic, so an unrecognised box type is a blank.
func TestBoxTypeStringNeverPanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		box  boxType
		want string
	}{
		{name: "regular", box: regular, want: "├"},
		{name: "last", box: last, want: "└"},
		{name: "between", box: between, want: "│"},
		{name: "after last", box: afterLast, want: " "},
		{name: "out of range", box: boxType(99), want: " "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.NotPanics(t, func() { assert.Equal(t, tc.want, tc.box.String()) })
		})
	}
}
