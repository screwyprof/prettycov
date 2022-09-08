# The binary name.
BINARY ?= prettycov

# This repo's root import path.
PKG := gitlab.com/screwyprof/prettycov

## DO NOT EDIT BELLOW THIS LINE
GO_FILES = $(shell find . -name "*.go" | grep -v vendor | uniq)
LOCAL_PACKAGES="gitlab.com/screwyprof/prettycov"

VERSION := $(shell git describe --abbrev=0 --tags 2> /dev/null || echo 'v0.0.0')+$(shell git rev-parse --short HEAD)

# warning: -w will disable runtime profiling and affect debugging
# see https://stackoverflow.com/questions/22267189/what-does-the-w-flag-mean-when-passed-in-via-the-ldflags-option-to-the-go-comman
LDFLAGS = -w -s -X main.version=$(VERSION)

## build statically on linux
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
	# see http://tbg.github.io/linking-golang-go-statically-cgo-testing
	LDFLAGS += -extldflags -static
endif

SHELL := bash

OK_COLOR=\033[32;01m
NO_COLOR=\033[0m
MAKE_COLOR=\033[36m%-20s\033[0m

IGNORE_COVERAGE_FOR=-e .*_gen.go -e backend-server.go -e  backend-types.go -e internal -e pkg/web3signer/client -e pkg/eth2/spec -e test_helpers.go -e .*test

all: build lint test ## build application, run linters and tests

build: ## build application
	@echo -e "$(OK_COLOR)==> Building application$(NO_COLOR)"
	go build -race -tags netgo -ldflags "$(LDFLAGS)" -o $(PWD)/$(BINARY) $(PWD)/cmd/...

fmt: ## format code
	@echo -e "$(OK_COLOR)==> Formatting$(NO_COLOR)"
	@gofumpt -l -w .
	@gci write $(GO_FILES) -s standard  -s default -s "prefix($(LOCAL_PACKAGES))"

test:
	@echo -e "$(OK_COLOR)==> Running tests$(NO_COLOR)"
	@set -euo pipefail && go test -json -v -race -count=1 -timeout=120s -cover -covermode atomic -coverprofile=coverage.tmp ./... | tparse -follow
	@set -euo pipefail && cat coverage.tmp | grep -v $(IGNORE_COVERAGE_FOR) > coverage.out && rm coverage.tmp

test-cover-txt: ## show plain coverage report in console
	@echo -e "$(OK_COLOR)==> Generating coverage report$(NO_COLOR)"
	@go tool cover -func coverage.out | tr -s '\t' ' ' | column -t -c2

test-cover-html: ## show html coverage report
	@echo -e "$(OK_COLOR)==> Generating coverage report$(NO_COLOR)"
	@go tool cover -html=coverage.out

test-cover-total: # show total coverage.out
	@echo -e "$(OK_COLOR)==> Total coverage:$(NO_COLOR)"
	@go tool cover -func coverage.out  | tail -n 1 | rev | cut -f1 | rev

test-cover-svg: # generate pretty coverage picture
	@go-cover-treemap -coverprofile coverage.out > coverage.svg
	@open coverage.svg

lint: ## run linters for current changes
	@echo -e "$(OK_COLOR)==> Linting current changes$(NO_COLOR)"
	golangci-lint  run ./...

lint-all: ## run linters
	@echo -e "$(OK_COLOR)==> Linting$(NO_COLOR)"
	golangci-lint run ./... --new-from-rev=""

install: ## install binary
	@echo -e "$(OK_COLOR)==> Installing binary$(NO_COLOR)"
	go install -ldflags "$(LDFLAGS)" $(PWD)/cmd/prettycov/...

deps: ## install deps
	@echo -e "$(OK_COLOR)==> Installing dependencies$(NO_COLOR)"
	go install mvdan.cc/gofumpt@v0.3.1
	go install github.com/daixiang0/gci@v0.4.3
	go install github.com/mfridman/tparse@v0.11.1
	go install github.com/nikolaydubina/go-cover-treemap@latest

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
.PHONY: lint lint-all install deps clean help
