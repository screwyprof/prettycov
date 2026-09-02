# The binary name.
BINARY ?= prettycov

## DO NOT EDIT BELLOW THIS LINE
GO_FILES := $(shell find . -name "*.go" -not -path "./.direnv/*" | grep -v vendor | uniq)
LOCAL_PACKAGES="github.com/screwyprof/prettycov"
COVERAGE := coverage.out

# ./VERSION is the single source of truth: flake.nix reads the same file, and `make release` tags
# from it. Dev builds still carry the commit, so binaries report e.g. v0.1.3+abc1234.
VERSION := v$(shell cat VERSION)+$(shell git rev-parse --short HEAD)

# warning: -w will disable runtime profiling and affect debugging
# see https://stackoverflow.com/questions/22267189/what-does-the-w-flag-mean-when-passed-in-via-the-ldflags-option-to-the-go-comman
LDFLAGS = -w -s -X main.version=$(VERSION)

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

OK_COLOR=\033[32;01m
NO_COLOR=\033[0m
MAKE_COLOR=\033[36m%-20s\033[0m

all: build lint test ## build application, run linters and tests

build: ## build application
	@echo -e "$(OK_COLOR)==> Building application$(NO_COLOR)"
	go build -race -tags netgo -ldflags "$(LDFLAGS)" -o $(PWD)/$(BINARY) $(PWD)/cmd/...

fmt: ## format code
	@echo -e "$(OK_COLOR)==> Formatting$(NO_COLOR)"
	@gofumpt -l -w .
	@gci write $(GO_FILES) -s standard  -s default -s "prefix($(LOCAL_PACKAGES))"

test: ## run tests and write the coverage profile
	@echo -e "$(OK_COLOR)==> Running tests$(NO_COLOR)"
	@go test -json -v -race -count=1 -timeout=120s -cover -covermode atomic -coverprofile=$(COVERAGE) ./... | tparse -follow

# The reports below read the profile, so they need one to exist. Depending on the file rather than
# on `test` means a report regenerates it when absent but reuses it when present, instead of
# re-running the whole suite every time you want to look at the numbers again.
$(COVERAGE):
	@$(MAKE) --no-print-directory test

test-cover-txt: $(COVERAGE) ## show plain coverage report in console
	@echo -e "$(OK_COLOR)==> Generating coverage report$(NO_COLOR)"
	@go tool cover -func $(COVERAGE) | tr -s '\t' ' ' | column -t -c2

# Written to a file rather than handed straight to a browser, so the report survives on a machine
# with no display instead of the target silently doing nothing. Same best-effort open as the SVG.
test-cover-html: $(COVERAGE) ## show html coverage report
	@echo -e "$(OK_COLOR)==> Generating coverage report$(NO_COLOR)"
	@go tool cover -html=$(COVERAGE) -o coverage.html
	@$(OPEN) coverage.html 2>/dev/null || echo "==> coverage.html written ($(OPEN) unavailable)"

# Statements, not lines: Go instruments statements, so this will not match a line-based service
# such as codecov.
test-cover-total: $(COVERAGE) ## show total coverage
	@echo -e "$(OK_COLOR)==> Total coverage:$(NO_COLOR)"
	@go tool cover -func $(COVERAGE) | tail -n 1 | rev | cut -f1 | rev

# Opening is best-effort: the SVG is the deliverable, and CI/containers have no display.
test-cover-svg: $(COVERAGE) ## generate pretty coverage picture
	@go-cover-treemap -coverprofile $(COVERAGE) > coverage.svg
	@$(OPEN) coverage.svg 2>/dev/null || echo "==> coverage.svg written ($(OPEN) unavailable)"

lint: ## run linters for current changes
	@echo -e "$(OK_COLOR)==> Linting current changes$(NO_COLOR)"
	golangci-lint  run ./...

lint-all: ## run linters
	@echo -e "$(OK_COLOR)==> Linting$(NO_COLOR)"
	golangci-lint run ./... --new-from-rev=""

install: ## install binary
	@echo -e "$(OK_COLOR)==> Installing binary$(NO_COLOR)"
	go install -ldflags "$(LDFLAGS)" $(PWD)/cmd/prettycov/...

# Versions live in go.mod's `tool` block, not here, so there is ONE place to bump them. The nix
# devShell already supplies these at the same versions; this target is for everyone who isn't in it.
deps: ## install deps
	@echo -e "$(OK_COLOR)==> Installing dependencies$(NO_COLOR)"
	go install tool

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
	@rm -rf ./prettycov

help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "$(MAKE_COLOR) %s\n", $$1, $$2}'

# To avoid unintended conflicts with file names, always add to .PHONY
# unless there is a reason not to.
# https://www.gnu.org/software/make/manual/html_node/Phony-Targets.html
.PHONY: all build fmt
.PHONY: test test-cover-txt test-cover-html test-cover-total test-cover-svg
.PHONY: lint lint-all install deps hooks nix-hash release clean help
