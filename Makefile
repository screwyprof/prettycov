# The binary name.
BINARY ?= prettycov

## DO NOT EDIT BELLOW THIS LINE
GO_FILES := $(shell find . -name "*.go" -not -path "./.direnv/*" | grep -v vendor | uniq)
# Fixtures are inputs too. Without them a changed profile or golden file leaves the report targets
# reading a coverage.out that predates it, and make calls the file up to date.
FIXTURES := $(shell find . -path "*/testdata/*" -type f -not -path "./.direnv/*")
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
fmt: require-golangci ## format code
	@echo -e "$(OK_COLOR)==> Formatting$(NO_COLOR)"
	@golangci-lint fmt ./...

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
	@go tool cover -func $(COVERAGE) | awk 'END{print $$NF}'

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
	golangci-lint  run ./...

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
release: ## tag a release from ./VERSION
	@v="v$$(cat VERSION)"; \
	if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "working tree is dirty; commit first"; exit 1; \
	fi; \
	if git rev-parse "$$v" >/dev/null 2>&1; then \
		echo "$$v already exists — bump ./VERSION first"; exit 1; \
	fi; \
	echo -e "$(OK_COLOR)==> Tagging $$v$(NO_COLOR)"; \
	git tag -a "$$v" -m "$$v" && git push origin "$$v"

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
.PHONY: lint lint-all install hooks nix-hash release clean help
