package main

import (
	"os"
	"path"
	"path/filepath"

	"github.com/screwyprof/prettycov"
)

func showReport(params flags) {
	dir := path.Dir(params.Profile)
	// file := path.Base(params.Profile)

	curDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	failOnError(err)

	failOnError(os.Chdir(dir))

	items, err := prettycov.ParseProfile(params.Profile)
	failOnError(err)

	tree := prettycov.Process(items, params.CurrentRoot, params.NewRoot)
	prettycov.DisplayTree(os.Stdout, tree, params.Depth)

	failOnError(os.Chdir(curDir))
}
