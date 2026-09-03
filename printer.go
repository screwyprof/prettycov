package prettycov

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
)

// Colour thresholds, in percent. Cosmetic: they grade a row at a glance and are deliberately not
// tied to any pass/fail decision.
const (
	poor = 50.0
	good = 80.0
)

// Base ANSI colours only. The 256-colour and truecolor ranges name an exact shade and so override
// whatever the user's terminal theme chose; these four are remapped by it, which is the point.
const (
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	reset  = "\x1b[0m"
)

// ColorMode says when to colour percentages. Auto is the zero value and the right answer almost
// always; the other two exist because a caller sometimes knows better than the heuristic, which
// is why every tool that colours output offers --color=auto|never|always.
type ColorMode int

const (
	// ColorAuto colours only when writing to a terminal that has not asked otherwise.
	ColorAuto ColorMode = iota
	// ColorNever never colours, whatever it is writing to.
	ColorNever
	// ColorAlways colours even into a pipe, for a caller that will render the escapes itself.
	ColorAlways
)

// Options controls how a tree is rendered. The zero value prints the top row alone, colouring it
// only if the destination is a terminal.
type Options struct {
	// Depth is how many levels to show below the top row, the way `tree -L` counts.
	Depth uint

	// Color decides whether percentages carry the terminal's own red, yellow and green.
	Color ColorMode
}

// DisplayTree writes tree as an indented report. A collapsed run of directories is the one row it
// renders as.
func DisplayTree(w io.Writer, tree *PathTree, opts Options) {
	displayTree(w, tree, opts.Depth, colorize(w, opts.Color), 0, " ", true)
}

// colorize resolves ColorAuto against the destination and the environment. NO_COLOR counts
// however it is set, including empty, per the convention at https://no-color.org.
func colorize(w io.Writer, mode ColorMode) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	case ColorAuto:
	}

	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}

	if os.Getenv("TERM") == "dumb" {
		return false
	}

	file, ok := w.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()

	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func displayTree(w io.Writer, tree *PathTree, depth uint, color bool, level uint, padding string, root bool) {
	if tree == nil || level > depth {
		return
	}

	// Sorted, because this output gets diffed between runs and map order is randomised.
	names := slices.Sorted(maps.Keys(tree.Children))

	for i, name := range names {
		label, node := collapse(name, tree.Children[name])

		_, _ = fmt.Fprintf(w, "%s%s - %s\n",
			padding+symbol(root, getBoxType(i, len(names))), label, formatRatio(node.Coverage, color))
		displayTree(w, node, depth, color, level+1,
			padding+symbol(root, childSymbol(i, len(names))), false)
	}
}

// collapse folds a run of directories that each hold nothing but the next one into a single row,
// so a module path does not spend three levels on "github.com", "owner", "repo" before reaching
// anything worth reading. A directory the profile named itself is never folded away, however few
// statements it holds — a package whose files declare none still deserves its own row.
func collapse(label string, node *PathTree) (string, *PathTree) {
	for !node.isPkg && len(node.Children) == 1 {
		for name, child := range node.Children {
			label, node = label+"/"+name, child
		}
	}

	return label, node
}

// formatRatio renders a package with no statements as "n/a" rather than a percentage. It used to
// print "NaN", which is what 0/0 produces in float division.
func formatRatio(stats CoverageStats, color bool) string {
	pct, ok := stats.Ratio()
	if !ok {
		// Nothing to cover is not a grade, so it is not coloured either.
		return "n/a"
	}

	text := fmt.Sprintf("%.2f", pct)
	if !color {
		return text
	}

	return grade(pct) + text + reset
}

func grade(pct float64) string {
	switch {
	case pct < poor:
		return red
	case pct < good:
		return yellow
	default:
		return green
	}
}

type BoxType int

const (
	Regular BoxType = iota
	Last
	AfterLast
	Between
)

// String renders the glyph for a box type. An unrecognised one is a blank rather than a panic:
// this draws a report, and nothing here is worth taking the process down for.
func (boxType BoxType) String() string {
	switch boxType {
	case Regular:
		return "\u251c" // ├
	case Last:
		return "\u2514" // └
	case AfterLast:
		return " "
	case Between:
		return "\u2502" // │
	}

	return " "
}

func getBoxType(index int, length int) BoxType {
	if index+1 == length {
		return Last
	}

	return Regular
}

func childSymbol(index int, length int) BoxType {
	if index+1 == length {
		return AfterLast
	}

	return Between
}

func symbol(root bool, boxType BoxType) string {
	if root {
		return ""
	}

	return boxType.String() + " "
}
