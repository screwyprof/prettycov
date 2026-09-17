# The binary name.
BINARY ?= prettycov

## DO NOT EDIT BELLOW THIS LINE
# Asked of git rather than found on disk, so the lists are the repository's files and not whatever
# else the working tree holds. A `find` walks ignored directories too, and anything checked out
# below the root — a scratch clone, a downloaded dataset — lands in the prerequisites: names with
# spaces then split into targets make has no rule for, and it stops before running anything.
#
# -co keeps files that are merely untracked, so a new one still triggers a rebuild. quotePath=false
# stops git C-quoting a non-ASCII name into "caf\303\251.go", which wildcard would then drop as a
# path that does not exist. wildcard itself drops what -c goes on listing after an unstaged delete.
#
# Two limits worth knowing: this needs a checkout, since without .git both lists come out empty
# (git says so, twice, on every invocation); and a tracked file with a space in its name is dropped
# rather than reported, because make splits prerequisites on whitespace whatever we hand it.
GIT_LS := git -c core.quotePath=false ls-files -co --exclude-standard
GO_FILES := $(wildcard $(shell $(GIT_LS) "*.go"))
# Fixtures are inputs too. Without them a changed profile or golden file leaves the report targets
# reading a coverage.out that predates it, and make calls the file up to date.
FIXTURES := $(wildcard $(shell $(GIT_LS) "*testdata/*"))
LOCAL_PACKAGES=github.com/screwyprof/prettycov
COVERAGE := coverage.out
# The same figure codecov.yml sets as its project target. Two places, because one is what a
# contributor sees before pushing and the other is what blocks the merge.
COVERAGE_FLOOR := 99
# Tracked Markdown only, so a vendored or downloaded .md is never linted.
MARKDOWN = $(shell $(GIT_LS) '*.md')
# Counter files from the binary tests, folded into $(COVERAGE) below.
COVERDATA := .covdata

# main_test.go spawns a binary, so it is tagged and run in a pass of its own. Also in .golangci.yml,
# which needs the tag to lint the file at all.
GO_TAGS := integration
# Every tool is fetched by `go run pkg@version` at the version named here, so this block is the
# whole toolchain. The `# renovate:` lines let the bot read a Makefile it would otherwise ignore —
# datasource=go makes it resolve each module against the proxy, the same place `go run` will.
# renovate: datasource=go depName=golang.org/x/vuln
GOVULNCHECK_VERSION := v1.8.0
# renovate: datasource=go depName=github.com/rillig/gobco
GOBCO_VERSION := v1.3.4
# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2
GOLANGCI_VERSION := v2.13.2
# vale-cli, not errata-ai: module moved at v3.20.0, old path still serves the tags and then refuses.
# renovate: datasource=go depName=github.com/vale-cli/vale/v3
VALE_VERSION := v3.21.0
# renovate: datasource=go depName=github.com/reviewdog/reviewdog
REVIEWDOG_VERSION := v0.21.1
# renovate: datasource=go depName=github.com/go-gremlins/gremlins
GREMLINS_VERSION := v0.6.0

# ./VERSION is the single source of truth: `make release` tags from it and the linker stamps it in.
# Dev builds still carry the commit, so binaries report e.g. v0.1.3+abc1234.
VERSION := v$(shell cat VERSION)+$(shell git rev-parse --short HEAD)

# warning: -w will disable runtime profiling and affect debugging
# see https://stackoverflow.com/questions/22267189/what-does-the-w-flag-mean-when-passed-in-via-the-ldflags-option-to-the-go-comman
LDFLAGS = -w -s -X github.com/screwyprof/prettycov/internal/app.version=$(VERSION)

## build statically on linux
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
	# see http://tbg.github.io/linking-golang-go-statically-cgo-testing
	LDFLAGS += -extldflags -static
endif

## `open` is macOS-only; the freedesktop equivalent is xdg-open. Same uname switch as above.
ifeq ($(UNAME_S),Darwin)
	OPEN := open
else
	OPEN := xdg-open
endif

# bash, not sh: `echo -e` below prints a literal "-e" under dash, which is /bin/sh on the Ubuntu
# runners. -e -o pipefail applies to every recipe line, so a failing command anywhere in a pipe
# fails the target — without it `go tool cover ... | column` reported success when cover errored.
SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

# `go test -coverprofile` writes the profile even when the suite fails, so without this a failed
# run leaves a partial coverage.out behind and the next report reads it and exits 0.
.DELETE_ON_ERROR:

# Nothing here gains from -j, and coverage.out has one producer that several targets can ask for.
# Serialising removes any chance of two runs writing that file at once.
.NOTPARALLEL:

OK_COLOR=\033[32;01m
NO_COLOR=\033[0m
MAKE_COLOR=\033[36m%-20s\033[0m

all: build lint test ## build application, run linters and tests

# No -race here: it is a test tool, not a build flag. It also needs cgo, which silently undid the
# static linking below — the binary came out dynamically linked against the host glibc — and cost
# a second of race-runtime startup on a program whose work takes a millisecond.
build: ## build application
	@echo -e "$(OK_COLOR)==> Building application$(NO_COLOR)"
	go build -tags netgo -ldflags "$(LDFLAGS)" -o $(PWD)/$(BINARY) $(PWD)/cmd/...

# `go run pkg@version`, as govulncheck, gobco, gremlins and reviewdog are: one convention, and the
# pinned version is what runs. Probing PATH first was faster in the devShell and quietly ran
# whatever version was installed there instead, which is a pin that lies.
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

# golangci-lint formats as well as reports: `fmt` applies the formatters block in .golangci.yml,
# which is gofumpt and gci — the same two this used to shell out to — plus golines, which the
# standalone pair never applied at all, so a 128-column line survived `make fmt` unchanged.
#
# The file list, not ./..., because `fmt` walks the tree to expand it and `lint` does not: run
# loads packages, and the go tool skips directories starting with _ or . on the way. So a checkout
# left under the root cost this target a minute per commit while lint stayed instant — 74469 files
# walked to format 25.
fmt: ## format code
	@echo -e "$(OK_COLOR)==> Formatting$(NO_COLOR)"
	@test -n "$(GO_FILES)" || { echo "no Go files; GO_FILES needs a git checkout"; exit 1; }
	@$(GOLANGCI) fmt $(GO_FILES)

# One recipe produces the profile, and it is a real file rule so make can tell when it is stale.
# The reports depend on the file rather than on `test`, so they rebuild it when a source has
# changed and reuse it otherwise, instead of re-running the suite to re-read the same numbers.
#
# Two passes because the second must run without -cover: under -cover, cmd/go points GOCOVERDIR at
# a directory of its own and never reads it back, so a spawned binary's counters are discarded
# (golang/go#66225). Without it the variable is ours.
#
# -race on both passes: the forked binary is built without it, but the harness runs its cases in
# parallel over a shared binary path and coverage directory, and no other target compiles that
# file at all.
#
# The counter check is for tag drift. "integration" lives here, in the build tag and in
# .golangci.yml, and nothing makes the three agree — without it a mismatch runs no tests, leaves
# the directory empty, and fails inside covdata naming neither the tag nor the tests.
#
# Appending merges, because readers of this format sum blocks they see twice.
$(COVERAGE): $(GO_FILES) $(FIXTURES)
	@echo -e "$(OK_COLOR)==> Running tests$(NO_COLOR)"
	@rm -rf $(COVERDATA) && mkdir -p $(COVERDATA)
	@go test -race -count=1 -shuffle=on -timeout=120s -cover -covermode atomic -coverpkg=./... -coverprofile=$@ ./...
	@GOCOVERDIR=$(PWD)/$(COVERDATA) go test -race -count=1 -shuffle=on -timeout=120s -tags=$(GO_TAGS) ./cmd/prettycov/
	@test -n "$$(ls -A $(COVERDATA) 2>/dev/null)" || \
		{ echo "no counters in $(COVERDATA): did the -tags=$(GO_TAGS) pass run any tests?"; exit 1; }
	@go tool covdata textfmt -i=$(COVERDATA) -o=$(COVERDATA)/binary.txt
	@tail -n +2 $(COVERDATA)/binary.txt >> $@

# `make test` must always run the suite, so it drops the profile first rather than letting make
# decide it is up to date.
test: ## run tests and write the coverage profile
	@rm -f $(COVERAGE)
	@$(MAKE) --no-print-directory $(COVERAGE)

test-cover-txt: $(COVERAGE) ## show plain coverage report in console
	@echo -e "$(OK_COLOR)==> Generating coverage report$(NO_COLOR)"
	@go tool cover -func $(COVERAGE) | tr -s '\t' ' ' | column -t

# Written to a file rather than handed straight to a browser, so the report survives on a machine
# with no display instead of the target silently doing nothing. Opening it is best-effort.
coverage.html: $(COVERAGE)
	@echo -e "$(OK_COLOR)==> Generating coverage report$(NO_COLOR)"
	@go tool cover -html=$< -o $@

test-cover-html: coverage.html ## show html coverage report
	@$(OPEN) $< 2>/dev/null || echo "==> $< written ($(OPEN) unavailable)"

# Statements, not lines: Go instruments statements, so this will not match a line-based service
# such as codecov.
test-cover-total: $(COVERAGE) ## show total coverage
	@echo -e "$(OK_COLOR)==> Total coverage:$(NO_COLOR)"
	@go run ./cmd/prettycov total --profile=$(COVERAGE)

# Go measures statements, not branches: `return a && b` is one statement, covered the moment it
# runs, whichever way it evaluates. gobco instruments the conditions themselves and says which
# were never true or never false. Pinned and run with `go run pkg@version`, which leaves go.mod
# and go.sum untouched, so this stays a tool you reach for rather than a dependency.
#
# Run against a copy holding only what git tracks. gobco copies the whole module root into its
# own temporary tree, with filepath.Walk and no exclusions (main.go:153), so anything sitting
# beside the source comes too: a gitignored _reference/ of cloned repositories made that 2.6GB,
# which filled /tmp and killed the run on ENOSPC. Same shape as the one that made `make fmt` walk
# 74,469 files — a tool reading the filesystem where the Go package graph was meant.
#
# Reachability-aware, so unlike a generic dependency scan it reports only what this binary can
# actually reach — no triage queue of advisories in code that never runs.
vulns: ## report known vulnerabilities reachable from this module
	@echo -e "$(OK_COLOR)==> Vulnerabilities$(NO_COLOR)"
	@go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# See GOLANGCI above: same convention, same reason.
VALE := go run github.com/vale-cli/vale/v3/cmd/vale@$(VALE_VERSION)

# `-diff` rather than tidy-then-`git diff`: it reports what would change without writing, so the
# gate cannot leave a dirty tree behind when it fails. Both files are checked in, so a stale one is
# a change that builds here and on no other machine.
tidy: ## check go.mod and go.sum are what the imports say
	@echo -e "$(OK_COLOR)==> Checking go.mod$(NO_COLOR)"
	@go mod tidy -diff

# Google's developer documentation style guide, as Vale packages it, with this repo's deviations
# recorded in .vale.ini. `vale sync` fetches the package into .vale/, which is gitignored, so the
# first run on a clean checkout downloads it.
docs-lint: .vale/Google ## check the Markdown against the prose style guide
	@echo -e "$(OK_COLOR)==> Linting docs$(NO_COLOR)"
	@$(VALE) $(MARKDOWN)

# A file rule, so the package is fetched once rather than on every gate run — `make check` then
# works offline, which an unconditional `vale sync` denied it.
.vale/Google: .vale.ini
	@echo -e "$(OK_COLOR)==> Fetching prose styles$(NO_COLOR)"
	@$(VALE) sync >/dev/null
	@touch $@

# The copy is $(GIT_LS), the list `fmt` already uses, so uncommitted work is measured. A worktree
# would be shorter and would silently report on HEAD instead.
cover-branches: ## report conditions never evaluated both ways
	@echo -e "$(OK_COLOR)==> Condition coverage$(NO_COLOR)"
	@test -n "$(GO_FILES)" || { echo "no Go files; this needs a git checkout"; exit 1; }
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT; \
	 $(GIT_LS) | tar -cf - -T - | (cd "$$tmp" && tar -xf -); \
	 for pkg in . ./internal/app ./internal/cli; do \
		(cd "$$tmp" && go run github.com/rillig/gobco@$(GOBCO_VERSION) $$pkg) | grep -v "^ok\b" || true; \
	 done

# Coverage says a line ran; it cannot say a test would have noticed the line being wrong. Gremlins
# changes the source — negating conditions, moving boundaries, flipping increments — and reports the
# mutants the suite failed to kill. A survivor is a line every test executes and none checks.
#
# Copied the same way as cover-branches, and for the same reason: gremlins works on its own copy of
# the module root, so a gitignored _reference/ comes with it and fills /tmp.
#
# --timeout-coefficient is the whole difference between a result and a wasted run. Gremlins times
# each mutant against a multiple of its baseline measurement, and the default left ours ~50ms
# against a suite needing 400: 105 of 123 mutants timed out and said nothing. At 30 the run takes
# twelve seconds and every mutant is decided.
#
# "Not covered" is worth reading but not chasing: those land on `switch { case <expr>: }` lines and
# package-level var initialisers, neither of which Go's cover instruments where gremlins looks.
# cover-branches is the one that answers for those.
mutate: ## report mutants the tests failed to kill
	@echo -e "$(OK_COLOR)==> Mutation testing$(NO_COLOR)"
	@test -n "$(GO_FILES)" || { echo "no Go files; this needs a git checkout"; exit 1; }
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT; \
	 $(GIT_LS) | tar -cf - -T - | (cd "$$tmp" && tar -xf -); \
	 (cd "$$tmp" && go run github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) \
		unleash --timeout-coefficient=30 .)

# Dogfooding: prettycov's own report on its own profile. Run from source rather than an installed
# binary, so a change to the printer shows up here before it is ever released.
test-cover-tree: $(COVERAGE) ## show the coverage tree (prettycov on itself)
	@go run ./cmd/prettycov report --profile=$(COVERAGE) --old=$(LOCAL_PACKAGES) --new=prettycov --depth=2

lint: ## run linters for current changes
	@echo -e "$(OK_COLOR)==> Linting current changes$(NO_COLOR)"
	$(GOLANGCI) run ./...

# CI only, and the same findings `lint` reports: reviewdog renders them as annotations on the pull
# request diff, which a log cannot. Piped rather than using reviewdog's own golangci-lint action,
# which downloads a version of its own choosing — the pin has to be the one that runs.
#
# Output aimed at a machine: no banner, no colour, no stats, or the errorformat has lines it cannot
# parse. Scoped to the diff, as `lint` is, because that is what an annotation can point at.
lint-annotate:
	@$(GOLANGCI) run ./... --output.text.print-issued-lines=false --output.text.colors=false --show-stats=false \
		| go run github.com/reviewdog/reviewdog/cmd/reviewdog@$(REVIEWDOG_VERSION) \
			-f=golangci-lint -name=golangci-lint -reporter=github-pr-check -fail-level=any

lint-all: ## run linters
	@echo -e "$(OK_COLOR)==> Linting$(NO_COLOR)"
	$(GOLANGCI) run ./... --new-from-rev=""

# Prints $(2) when the gate passes, the whole output when it fails. Filtering both ways hid two
# failures: govulncheck's advisory, and a Vale "1 error" against a pattern wanting "errors".
define summarise
out=$$($(MAKE) --no-print-directory $(1) 2>&1) || { echo "$$out"; exit 1; }; \
	echo "$$out" | grep -E '$(2)' || { echo "$$out"; exit 1; }
endef

# check runs every gate and prints one line per figure, so a PR description quotes the tools rather
# than being retyped from them. Six descriptions in this repo have claimed numbers the tree did not
# give, all of them hand-copied from these same targets.
#
# The binary is a gate too: it is built with netgo and static linking that `go test` never exercises,
# so asking it for its version proves the artifact runs and not merely that the package compiles.
#
# gobco prints the same sentence for each package and nothing in it says which, so the two condition
# lines are labelled here. That is the one thing copying by hand could not get wrong and reading the
# output could — and the labels are positional, so the count is asserted: cover-branches swallows a
# failed gobco run with `|| true`, and one surviving line would otherwise be labelled "# root"
# whichever package it came from, in a block whose whole purpose is to be pasted somewhere.
#
# Every line is a pipe, so without pipefail the status would be grep's and a failing gate would
# still print a clean-looking block and exit 0. Scoped to this target, so no other recipe changes.
check: SHELL := /usr/bin/env bash
check: .SHELLFLAGS := -o pipefail -c
# The coverage line is a gate, not a figure: it used to print the total and assert nothing, so
# `make check` passed at any coverage while codecov failed the pull request at 99%. Same bar now,
# said in both places, and asserted by the tool this repository is.
check: ## run every quality gate and print the block to paste into a PR description
	@echo -e "$(OK_COLOR)==> Checking$(NO_COLOR)" >&2
	@echo '$$ make build'
	@$(MAKE) --no-print-directory build >/dev/null
	@$(PWD)/$(BINARY) --version
	@echo; echo '$$ make test'
	@$(MAKE) --no-print-directory test >/dev/null
	@go run ./cmd/prettycov report --profile=$(COVERAGE) --old=$(LOCAL_PACKAGES) --new=prettycov \
		--depth=2 --files --hide-covered --fail-under=$(COVERAGE_FLOOR)
	@echo; echo '$$ make lint-all'
	@$(call summarise,lint-all,^[0-9]+ issues\.)
	@echo; echo '$$ make tidy'
	@$(call summarise,tidy,.)
	@echo; echo '$$ make vulns'
	@$(call summarise,vulns,No vulnerabilities|Vulnerability #)
	@echo; echo '$$ make docs-lint'
	@$(call summarise,docs-lint,[0-9]+ errors?)
	@echo; echo '$$ make mutate'
	@$(call summarise,mutate,^(Killed:|Test efficacy:))
	@echo; echo '$$ make cover-branches'
	@$(MAKE) --no-print-directory cover-branches 2>&1 | grep '^Condition coverage:' \
		| awk 'NR==1 {print $$0 "    # root"} NR==2 {print $$0 "    # internal/app"} \
		       NR==3 {print $$0 "    # internal/cli"} \
		       END {if (NR != 3) {print "cover-branches reported " NR " packages, wanted 3" > "/dev/stderr"; exit 1}}'

install: ## install binary
	@echo -e "$(OK_COLOR)==> Installing binary$(NO_COLOR)"
	go install -ldflags "$(LDFLAGS)" $(PWD)/cmd/prettycov/...

# ./VERSION holds the last released version — bump it, then run this.
release: ## tag a release from ./VERSION and publish it to the module proxy
	@v="v$$(cat VERSION)"; \
	if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "working tree is dirty; commit first"; exit 1; \
	fi; \
	echo -e "$(OK_COLOR)==> Tagging $$v$(NO_COLOR)"; \
	git tag -a "$$v" -m "$$v"; \
	git push origin "$$v"
	@$(MAKE) --no-print-directory publish \
		|| { echo "the tag is pushed; rerun just: make publish"; exit 1; }

# Asks the proxy to fetch the version, so pkg.go.dev lists it now instead of when the first user
# pulls it through. Step 6 of https://go.dev/doc/modules/publishing, verbatim.
#
# Best-effort, and not worth hardening: it answers from the module cache without asking anyone if
# the version is already there, which GOPRIVATE, GOPROXY=direct or a mirror can arrange. The green
# run then publishes nothing and the release waits for someone else's first fetch, which is where
# it would have been without this target at all.
publish: ## request ./VERSION from the module proxy, so pkg.go.dev indexes it
	@v="v$$(cat VERSION)"; \
	echo -e "$(OK_COLOR)==> Publishing $$v to the module proxy$(NO_COLOR)"; \
	GOPROXY=https://proxy.golang.org go list -m "$$(go list -m)@$$v"

# The nix devShell registers this on entry; this target is for everyone else. Needs pre-commit
# on PATH (pip install pre-commit / brew install pre-commit).
hooks: ## install git pre-commit hooks
	@echo -e "$(OK_COLOR)==> Installing git hooks$(NO_COLOR)"
	@pre-commit install

clean: ## cleans-up artifacts
	@echo -e "$(OK_COLOR)==> Cleaning up$(NO_COLOR)"
	@rm -rf ./coverage.*
	@rm -rf ./$(COVERDATA)
	@rm -rf ./prettycov

help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "$(MAKE_COLOR) %s\n", $$1, $$2}'

# To avoid unintended conflicts with file names, always add to .PHONY
# unless there is a reason not to.
# https://www.gnu.org/software/make/manual/html_node/Phony-Targets.html
.PHONY: all build fmt
.PHONY: test cover-branches mutate test-cover-txt test-cover-html test-cover-total test-cover-tree
.PHONY: lint lint-annotate lint-all vulns docs-lint tidy check install hooks release publish clean help
