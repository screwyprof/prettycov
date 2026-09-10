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
GOBCO_VERSION := v1.3.4

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
# The last step folds in the binary tests: they run a compiled prettycov, so its execution is
# absent from the suite's own profile. Appending merges, because readers of this format sum blocks
# they see twice. Guarded — `go test -run` filtered to other tests writes no counters at all.
$(COVERAGE): $(GO_FILES) $(FIXTURES)
	@echo -e "$(OK_COLOR)==> Running tests$(NO_COLOR)"
	@rm -rf $(COVERDATA) && mkdir -p $(COVERDATA)
	@PRETTYCOV_COVERDIR=$(PWD)/$(COVERDATA) \
		go test -race -count=1 -timeout=120s -cover -covermode atomic -coverprofile=$@ ./...
	@if [ -n "$$(ls -A $(COVERDATA) 2>/dev/null)" ]; then \
		go tool covdata textfmt -i=$(COVERDATA) -o=$(COVERDATA)/binary.txt && \
		tail -n +2 $(COVERDATA)/binary.txt >> $@; \
	fi

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
cover-branches: ## report conditions never evaluated both ways
	@echo -e "$(OK_COLOR)==> Condition coverage$(NO_COLOR)"
	@for pkg in . ./internal/app; do \
		go run github.com/rillig/gobco@$(GOBCO_VERSION) $$pkg | grep -v "^ok\b" || true; \
	done

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
#
# The push is the point of no return, so a failed one is sorted out rather than reported. Origin
# has nothing: the local tag goes, so a rerun tags again instead of hitting the guard below and
# being told to bump ./VERSION for a release that never left the machine. Origin has this very
# tag: the push landed and said otherwise, so the tag stays and only publishing is left. Origin
# has that name on another commit: this release did not happen, and the local tag would trip the
# guard on every rerun, so it goes and ./VERSION needs bumping. Origin cannot be reached: none of
# those is known, and deleting a tag that may well have landed is what mints a second object
# origin rejects for good, so it stays.
#
# `git tag -a` ends in `;` rather than `&&`: .SHELLFLAGS carries -e, so a failed tag stops the
# recipe right there, where `&&` would hand the failure to the `||` below and roll back a tag that
# was never made.
release: ## tag a release from ./VERSION and publish it to the module proxy
	@v="v$$(cat VERSION)"; \
	if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "working tree is dirty; commit first"; exit 1; \
	fi; \
	if git rev-parse "$$v" >/dev/null 2>&1; then \
		echo "$$v already exists — bump ./VERSION first"; exit 1; \
	fi; \
	echo -e "$(OK_COLOR)==> Tagging $$v$(NO_COLOR)"; \
	git tag -a "$$v" -m "$$v"; \
	git push origin "$$v" || { \
		probe=0; git ls-remote --exit-code --tags origin "$$v" >/dev/null 2>&1 || probe=$$?; \
		if [ $$probe = 2 ]; then \
			git tag -d "$$v"; \
		elif [ $$probe != 0 ]; then \
			echo "cannot reach origin to see whether $$v landed, so the local tag stays"; \
			echo "delete it yourself if it did not: git tag -d $$v"; \
		elif [ "$$(git ls-remote --tags origin "$$v^{}" | cut -f1)" = "$$(git rev-parse "$$v^{}")" ]; then \
			echo "origin already has $$v, at this commit; rerun just: make publish"; \
		else \
			git tag -d "$$v"; \
			echo "origin already has $$v, at another commit — this release did not happen; bump ./VERSION"; \
		fi; \
		exit 1; }
	@$(MAKE) --no-print-directory publish \
		|| { echo "the tag is pushed; rerun just: make publish"; exit 1; }

# Step 6 of https://go.dev/doc/modules/publishing: ask the proxy for the version. proxy.golang.org
# caches a version the first time anyone asks for it, index.golang.org lists what the proxy has
# learned, and pkg.go.dev builds from that — so a release nobody asks for stays unpublished. Its
# own target because the tag is already pushed by the time it runs: if this fails, `make publish`
# retries it, where `make release` would stop at the tag that now exists.
#
# Into a throwaway GOMODCACHE, which is what makes this a request at all: the go command answers
# from the cache once a version is local, so anyone who smoke-tested the release first would get a
# green run and nothing published. GOPRIVATE and GONOPROXY are cleared for the same reason, either
# one matching this module sends the go command straight past the proxy to the origin.
#
# The go tool rather than a hand-made HTTP request: it escapes the uppercase in a module path
# itself, needs nothing on PATH that building this repo does not already need, and phrases a
# failure in the proxy's own words — a v2 without a /v2 module path, a go.mod it cannot read, or
# the plain 404 that means it has not fetched the tag yet. Not -json, which reports a failure as a
# field and can still exit 0, where this target's whole job is to fail when nothing was published.
publish: ## request ./VERSION from the module proxy, so pkg.go.dev indexes it
	@v="v$$(cat VERSION)"; \
	echo -e "$(OK_COLOR)==> Publishing $$v to the module proxy$(NO_COLOR)"; \
	cache=$$(mktemp -d); rc=0; \
	out=$$(GOMODCACHE=$$cache GOPROXY=https://proxy.golang.org GOPRIVATE= GONOPROXY= GOFLAGS= \
		go mod download "$$(go list -m)@$$v" 2>&1) || rc=$$?; \
	GOMODCACHE=$$cache go clean -modcache || true; rm -rf "$$cache" || true; \
	if [ $$rc = 0 ]; then \
		echo "  proxy has it; index.golang.org and pkg.go.dev follow"; exit 0; \
	fi; \
	printf '%s\n' "$$out" | sed 's/^/  /'; \
	case "$$out" in \
	*"404 Not Found"*) \
		echo "  a 404 naming no fault is the proxy remembering a miss from before the tag existed;"; \
		echo "  it holds that for up to 30 minutes — wait it out and rerun: make publish";; \
	esac; \
	exit 1

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
.PHONY: test cover-branches test-cover-txt test-cover-html test-cover-total test-cover-tree
.PHONY: lint lint-all install hooks nix-hash release publish clean help
