package prettycov

import "regexp"

// A Rename is a root package path and what to shorten it to. One value because the two are only
// ever set, validated, reported and applied together: either alone does nothing.
type Rename struct {
	From, To string
}

// Wanted reports whether a rename was asked for at all, which is what separates "not asked for"
// from "asked for and did not happen".
func (r Rename) Wanted() bool { return r.From != "" }

// A Request is what to measure: which profile, and the things that change what is in it. How the
// answer is drawn — depth, files, counts, colour — is Options, and is not here, because those
// change what is shown rather than what is counted.
type Request struct {
	Profile string
	Rename  Rename
	Exclude []*regexp.Regexp
}

// An Outcome is how a measurement turned out. Exactly one of these is true of any run, which is why
// it is one value and not a tree beside a bool beside a reason: those could spell twelve states
// where only four mean anything.
type Outcome int

const (
	// Measured is a run with a tree.
	Measured Outcome = iota
	// NoStatements is a profile holding nothing to cover. Decided before any pattern or root is
	// judged: an empty profile has nothing for either to match, so every one of them would look
	// stale — a good rename named as the fault when the profile is what is empty.
	NoStatements
	// ExcludedAway is a profile that held statements until the patterns ran.
	ExcludedAway
	// RootMissed is a rename naming a package the profile does not hold — a typo, or a module path
	// that has moved. Only the matching can catch it.
	RootMissed
)

// A Measurement is what a profile and the request that read it produced.
//
// The tree is unexported and reachable only through Tree, which hands it back with whether there is
// one: a caller cannot read a nil tree without being told, and cannot assemble a Measurement whose
// outcome and tree disagree.
type Measurement struct {
	// Exclusions is what each pattern took, whatever the outcome — a pattern that emptied the
	// profile is the case a caller most wants to report.
	Exclusions []Exclusion

	tree    *PathTree
	outcome Outcome
}

// Outcome is how the run turned out.
func (m Measurement) Outcome() Outcome { return m.outcome }

// Tree is the measured tree, and whether there is one. There is one exactly when Outcome is
// Measured.
func (m Measurement) Tree() (*PathTree, bool) { return m.tree, m.outcome == Measured }

// Measure reads a profile and applies what decides its contents, in the one order that is correct.
//
// Statements are counted before any flag is judged; the root is matched against the whole profile
// rather than against what the patterns left, so a pattern that took every file under a good root
// is not reported as a bad root; the tree is built last, from what survived both.
//
// The error is the profile being unreadable. Everything else is an Outcome, because a caller may
// want to report it and carry on.
//
// It does not change directory. It used to chdir to the profile's directory and then open the path
// it was given, which meant any relative path with a directory component failed to resolve.
func Measure(req Request) (Measurement, error) {
	items, err := ParseProfile(req.Profile)
	if err != nil {
		return Measurement{}, err
	}

	if !anyStatements(items) {
		return Measurement{outcome: NoStatements}, nil
	}

	kept, excluded := Exclude(items, req.Exclude)
	shortened, renamed := Shorten(kept, req.Rename.From, req.Rename.To)

	if rootMissed(req.Rename, items, renamed) {
		return Measurement{Exclusions: excluded, outcome: RootMissed}, nil
	}

	tree := Process(shortened)
	if _, ok := tree.Percentage(); !ok {
		return Measurement{Exclusions: excluded, outcome: ExcludedAway}, nil
	}

	return Measurement{Exclusions: excluded, tree: tree, outcome: Measured}, nil
}

// rootMissed reports whether the rename named a package the profile does not hold.
//
// renamed is a shortcut, not a second reason: Exclude only drops files, never renames them, so
// anything it left that matched the root is in the profile too and HasRoot would agree. It keeps
// even that scan off the path where the rename worked, which is every run that is not a mistake.
func rootMissed(r Rename, items []FileCoverage, renamed int) bool {
	if !r.Wanted() || renamed > 0 {
		return false
	}

	return !HasRoot(items, r.From)
}

// anyStatements reports whether the profile holds anything to cover. Statements rather than files:
// cmd/cover emits blocks declaring none, so a profile can name files and still be empty.
func anyStatements(files []FileCoverage) bool {
	for _, f := range files {
		if f.Coverage.Total() > 0 {
			return true
		}
	}

	return false
}
