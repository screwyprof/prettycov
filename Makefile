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
# Counter files from the binary tests, folded into $(COVERAGE) below.
COVERDATA := .covdata

# main_test.go spawns a binary, so it is tagged and run in a pass of its own. Also in .golangci.yml,
# which needs the tag to lint the file at all.
GO_TAGS := integration
GOBCO_VERSION := v1.3.4
GREMLINS_VERSION := v0.6.0

# ./VERSION is the single source of truth: flake.nix reads the same file, and `make release` tags
# from it. Dev builds still carry the commit, so binaries report e.g. v0.1.3+abc1234.
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

# golangci-lint comes from the devShell or the developer's own install, not go.mod, so the targets
# needing it say where to get it rather than dying with "command not found".
GOLANGCI_MISSING := golangci-lint not found. Enter the nix devShell, or install v2.13.1 (the version CI pins) from https://golangci-lint.run/docs/welcome/install/

require-golangci:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "$(GOLANGCI_MISSING)"; exit 1; }

# golangci-lint formats as well as reports: `fmt` applies the formatters block in .golangci.yml,
# which is gofumpt and gci — the same two this used to shell out to — plus golines, which the
# standalone pair never applied at all, so a 128-column line survived `make fmt` unchanged.
#
# The file list, not ./..., because `fmt` walks the tree to expand it and `lint` does not: run
# loads packages, and the go tool skips directories starting with _ or . on the way. So a checkout
# left under the root cost this target a minute per commit while lint stayed instant — 74469 files
# walked to format 25.
fmt: require-golangci ## format code
	@echo -e "$(OK_COLOR)==> Formatting$(NO_COLOR)"
	@test -n "$(GO_FILES)" || { echo "no Go files; GO_FILES needs a git checkout"; exit 1; }
	@golangci-lint fmt $(GO_FILES)

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
	@go test -race -count=1 -timeout=120s -cover -covermode atomic -coverprofile=$@ ./...
	@GOCOVERDIR=$(PWD)/$(COVERDATA) go test -race -count=1 -timeout=120s -tags=$(GO_TAGS) ./cmd/prettycov/
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
	@go run ./cmd/prettycov -total $(COVERAGE)

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
# The copy is $(GIT_LS), the list `fmt` already uses, so uncommitted work is measured. A worktree
# would be shorter and would silently report on HEAD instead.
cover-branches: ## report conditions never evaluated both ways
	@echo -e "$(OK_COLOR)==> Condition coverage$(NO_COLOR)"
	@test -n "$(GO_FILES)" || { echo "no Go files; this needs a git checkout"; exit 1; }
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT; \
	 $(GIT_LS) | tar -cf - -T - | (cd "$$tmp" && tar -xf -); \
	 for pkg in . ./internal/app; do \
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
	@go run ./cmd/prettycov -profile=$(COVERAGE) -old=$(LOCAL_PACKAGES) -new=prettycov -depth=2

lint: require-golangci ## run linters for current changes
	@echo -e "$(OK_COLOR)==> Linting current changes$(NO_COLOR)"
	golangci-lint run ./...

lint-all: require-golangci ## run linters
	@echo -e "$(OK_COLOR)==> Linting$(NO_COLOR)"
	golangci-lint run ./... --new-from-rev=""

install: ## install binary
	@echo -e "$(OK_COLOR)==> Installing binary$(NO_COLOR)"
	go install -ldflags "$(LDFLAGS)" $(PWD)/cmd/prettycov/...

# buildGoModule needs a fixed-output hash for the module set, and nix only reveals the correct one
# by failing a build with a wrong one. So: write a known-bad hash, read the `got:` line, write that.
# `sed -i.bak` rather than `sed -i` because BSD sed (macOS) requires the suffix.
FAKE_HASH := sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=

nix-hash: ## recompute flake.nix vendorHash (run after go.mod/go.sum change)
	@echo -e "$(OK_COLOR)==> Recomputing vendorHash$(NO_COLOR)"
	@sed -i.bak -E 's|vendorHash = "sha256-[^"]*"|vendorHash = "$(FAKE_HASH)"|' flake.nix
	@hash=$$( { nix build --no-link .#default 2>&1 || true; } | grep -oE 'sha256-[A-Za-z0-9+/=]{44}' | grep -v '^$(FAKE_HASH)$$' | head -1 || true); \
	if [ -z "$$hash" ]; then \
		echo "could not determine vendorHash; restoring"; mv flake.nix.bak flake.nix; exit 1; \
	fi; \
	sed -i.bak2 -E "s|vendorHash = \"$(FAKE_HASH)\"|vendorHash = \"$$hash\"|" flake.nix; \
	rm -f flake.nix.bak flake.nix.bak2; \
	echo "vendorHash = $$hash"

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
.PHONY: all build fmt require-golangci
.PHONY: test cover-branches mutate test-cover-txt test-cover-html test-cover-total test-cover-tree
.PHONY: lint lint-all install hooks nix-hash release publish clean help
