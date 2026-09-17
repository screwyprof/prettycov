package prettycov

import (
	"regexp"
	"slices"
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

// NamesNoPackage reports a source that cannot match anything — separators and nothing else, which
// `--old=$(MODULE)/` spells with MODULE unset. Here rather than in a caller, so it cannot disagree
// with Shorten's own trimming.
func (r Rename) NamesNoPackage() bool {
	return r.Wanted() && strings.TrimRight(r.From, "/") == ""
}

// Half reports one side of a rename given without the other. A value rule, not a presence one:
// `--old=$(MODULE) --new=.` with MODULE unset supplies both flags and still renames nothing.
func (r Rename) Half() bool { return (r.From == "") != (r.To == "") }

// A Request is what to measure: the profile, and what changes its contents. How the answer is drawn
// is Options — that changes what is shown rather than what is counted.
type Request struct {
	Profile string
	Rename  Rename
	Exclude []*regexp.Regexp
}

// An Outcome is how a measurement turned out. Exactly one is true of any run — one value rather
// than a tree beside a bool beside a reason, which could spell twenty states where five mean
// anything.
type Outcome int

const (
	// Unmeasured is the zero Outcome, carried by the Measurement returned beside an error.
	Unmeasured Outcome = iota
	// Measured is a run with a tree.
	Measured
	// NoStatements is a profile holding nothing to cover. Judged before any pattern or root, since
	// an empty profile makes every one of them look stale.
	NoStatements
	// ExcludedAway is a profile that held statements until the patterns ran.
	ExcludedAway
	// RootMissed is a rename naming a package the profile does not hold. Only the matching catches it.
	RootMissed
)

// A Measurement is what a profile and the request that read it produced. The tree is the whole of
// the state — Measured is derived from it, not stored beside it, so there is no pair to fall out of
// step. Holding both let the zero value answer Tree with (nil, true).
type Measurement struct {
	// Exclusions is what each pattern took, whatever the outcome.
	Exclusions []Exclusion

	tree *PathTree
	// why there is no tree, read only when there is none. Measured is never written here.
	why Outcome
}

// Outcome is how the run turned out.
func (m Measurement) Outcome() Outcome {
	if m.tree != nil {
		return Measured
	}

	return m.why
}

// Tree is the measured tree, and whether there is one. True exactly when Outcome is Measured.
func (m Measurement) Tree() (*PathTree, bool) { return m.tree, m.tree != nil }

// Measure reads a profile and applies what decides its contents, in the one order that is correct:
// statements counted before any flag is judged, the root matched against the whole profile rather
// than what the patterns left, the tree built last.
//
// The error is the profile being unreadable. Everything else is an Outcome, since a caller may want
// to report it and carry on.
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
