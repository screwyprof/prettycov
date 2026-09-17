package prettycov_test

import (
	"fmt"
	"os"
	"regexp"

	"github.com/screwyprof/prettycov"
)

// The profile every example reads. `go test -coverprofile` writes one of these; it is the same
// shape, three files across two packages.
const exampleProfile = `mode: set
example.com/m/pkg/logger/logger.go:10.2,12.3 6 1
example.com/m/pkg/logger/logger.go:14.2,15.3 2 0
example.com/m/web/handler.go:20.2,24.3 8 1
example.com/m/web/mock_test_helper.go:30.2,31.3 4 0
`

// writeExampleProfile puts the constant somewhere ParseProfile can open it, and leaves it there:
// an example has no testing.TB to hang a cleanup on, and the OS reaps its own temp directory. One
// shape rather than two, since a caller that must remember to defer is a caller that can forget.
func writeExampleProfile() string {
	f, err := os.CreateTemp("", "prettycov-example-*.out")
	if err != nil {
		panic(err)
	}

	if _, err := f.WriteString(exampleProfile); err != nil {
		panic(err)
	}

	if err := f.Close(); err != nil {
		panic(err)
	}

	return f.Name()
}

// Measure is the whole of reading a profile: it parses, applies the rename and the exclusions in
// the one order that is correct, and reports how the run turned out.
func ExampleMeasure() {
	path := writeExampleProfile()

	got, err := prettycov.Measure(prettycov.Request{Profile: path})
	if err != nil {
		panic(err)
	}

	tree, ok := got.Tree()
	if !ok {
		panic("no tree: " + fmt.Sprint(got.Outcome()))
	}

	pct, _ := tree.Percentage()
	fmt.Println(pct)

	// Output:
	// 70.00
}

// DisplayTree draws the packages and what they cover. The error is the destination's: a report
// written to a full disk is not a report that was printed.
func ExampleDisplayTree() {
	path := writeExampleProfile()

	got, err := prettycov.Measure(prettycov.Request{Profile: path})
	if err != nil {
		panic(err)
	}

	tree, _ := got.Tree()

	if _, err := prettycov.DisplayTree(os.Stdout, tree, prettycov.Options{Depth: 1}); err != nil {
		panic(err)
	}

	// Output:
	//  example.com/m - 70.00
	//  ├ pkg/logger - 75.00
	//  └ web - 66.67
}

// A rename shortens the root in every label, and the count says whether it matched anything, which
// is the only way to tell "did not rename" from "was not asked to".
func ExampleShorten() {
	files, err := prettycov.ParseProfile(writeExampleProfile())
	if err != nil {
		panic(err)
	}

	shortened, renamed := prettycov.Shorten(files, "example.com/m", "m")

	// Three, not four: ParseProfile keys a profile by filename, so logger.go's two blocks are one
	// FileCoverage by the time Shorten sees them.
	fmt.Println(renamed, shortened[0].File)

	// Output:
	// 3 m/pkg/logger/logger.go
}

// Exclude drops what was never meant to be counted, before anything is totalled, and reports what
// each pattern took so a typo cannot pass for a clean run.
func ExampleExclude() {
	files, err := prettycov.ParseProfile(writeExampleProfile())
	if err != nil {
		panic(err)
	}

	kept, excluded := prettycov.Exclude(files, []*regexp.Regexp{regexp.MustCompile(`_test_helper\.go$`)})

	fmt.Println(len(kept), "files kept")

	for _, ex := range excluded {
		fmt.Printf("%s took %d statements in %d files\n", ex.Pattern, ex.Statements, ex.Files)
	}

	// Output:
	// 2 files kept
	// _test_helper\.go$ took 4 statements in 1 files
}

// A path is spelled as the report draws it. Get resolves that spelling, including under the module
// root the report collapsed away, so a path copied off a row works as well as the profile's own.
func ExamplePathTree_Get() {
	path := writeExampleProfile()

	got, err := prettycov.Measure(prettycov.Request{Profile: path})
	if err != nil {
		panic(err)
	}

	tree, _ := got.Tree()

	full, _ := tree.Get("example.com/m/pkg/logger").Percentage()
	short, _ := tree.Get("pkg/logger").Percentage()

	fmt.Println(full, short)

	// Output:
	// 75.00 75.00
}
