package prettycov

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A Rename is a root package path and what to shorten it to. One value, since either alone does
// nothing.
type Rename struct {
	From, To string
}

// Wanted reports whether a rename was asked for at all, which is what separates "not asked for"
// from "asked for and did not happen".
func (r Rename) Wanted() bool { return r.From != "" }

// NamesNoPackage reports a source that cannot match anything: separators and nothing else, which
// `--old=$(MODULE)/` spells with MODULE unset. Here rather than in a caller, so it cannot disagree
// with Shorten's own trimming.
func (r Rename) NamesNoPackage() bool {
	return r.Wanted() && strings.TrimRight(r.From, "/") == ""
}

// Half reports one side of a rename given without the other. A value rule, not a presence one:
// `--old=$(MODULE) --new=.` with MODULE unset supplies both flags and still renames nothing.
func (r Rename) Half() bool { return (r.From == "") != (r.To == "") }

// A Request is what to measure: the profile, and what changes its contents. How the answer is drawn
// is Options, which changes what is shown rather than what is counted.
type Request struct {
	Profile string
	Rename  Rename
	Exclude []*regexp.Regexp
}

// A Failure is why a measurement produced no tree. Only the three, because "there is one" is
// [Measurement.Tree]'s bool: a type spelling that too would carry two values no run can reach, and
// every switch over it would need an arm that cannot run.
type Failure int

// From one, so the zero Failure names nothing: it is what the Measurement beside an error carries,
// and that caller has the error. Leaving it unnamed keeps it out of every switch.
const (
	// NoStatements is a profile holding nothing to cover. Judged before any pattern or root, since
	// an empty profile makes every one of them look stale.
	NoStatements Failure = iota + 1
	// ExcludedAway is a profile that held statements until the patterns ran.
	ExcludedAway
	// RootMissed is a rename naming a package the profile does not hold. Only the matching catches it.
	RootMissed
)

// String names the condition in this package's own words. A front end says which flag caused it,
// which is more than this type knows: the domain has no command line to name.
func (f Failure) String() string {
	switch f {
	case NoStatements:
		return "no statements to cover"
	case ExcludedAway:
		return "the exclusions left nothing to report"
	case RootMissed:
		return "the rename matched nothing"
	}

	// The zero, which no run produces, in the shape stringer gives an unnamed value.
	return "Failure(" + strconv.Itoa(int(f)) + ")"
}

// A Measurement is what a profile and the request that read it produced. The tree is the only
// state, and whether there is one is read off it rather than stored beside it, so there is no pair
// to fall out of step. Holding both once let the zero value answer Tree with (nil, true), which is
// the one thing this type promises cannot happen.
type Measurement struct {
	// Exclusions is what each pattern took, whatever the outcome.
	Exclusions []Exclusion

	tree *PathTree
	// why there is no tree, read only when there is none.
	why Failure
}

// Failure is why there is no tree. Read it when [Measurement.Tree] reports false; a run that
// produced one has nothing to say here.
func (m Measurement) Failure() Failure { return m.why }

// Tree is the measured tree, and whether there is one.
func (m Measurement) Tree() (*PathTree, bool) { return m.tree, m.tree != nil }

// Measure reads a profile and applies what decides its contents, in the one order that is correct:
// statements counted before any flag is judged, the root matched against the whole profile rather
// than what the patterns left, the tree built last.
//
// The error is the profile being unreadable. Everything else comes back as a [Failure], since a
// caller may want to report it and carry on.
func Measure(req Request) (Measurement, error) {
	items, err := ParseProfile(req.Profile)
	if err != nil {
		return Measurement{}, err
	}

	if !anyStatements(items) {
		return Measurement{why: NoStatements}, nil
	}

	kept, excluded := Exclude(items, req.Exclude)
	shortened, renamed := Shorten(kept, req.Rename.From, req.Rename.To)

	if rootMissed(req.Rename, items, renamed) {
		return Measurement{Exclusions: excluded, why: RootMissed}, nil
	}

	tree := Process(shortened)
	if _, ok := tree.Percentage(); !ok {
		return Measurement{Exclusions: excluded, why: ExcludedAway}, nil
	}

	return Measurement{Exclusions: excluded, tree: tree}, nil
}

// rootMissed reports whether the rename named a package the profile does not hold. renamed is a
// shortcut, not a second reason: Exclude only drops files, so HasRoot would agree.
func rootMissed(r Rename, items []FileCoverage, renamed int) bool {
	if !r.Wanted() || renamed > 0 {
		return false
	}

	return !HasRoot(items, r.From)
}

// anyStatements reports whether the profile holds anything to cover. Statements rather than files:
// cmd/cover emits blocks declaring none.
func anyStatements(files []FileCoverage) bool {
	return slices.ContainsFunc(files, func(f FileCoverage) bool { return f.Coverage.Total() > 0 })
}
