# Makefile for orcli.
#
# This file is written in the syntax common to BSD make and GNU make, because
# the port host uses BSD make. That rules out several conveniences that appear
# in almost every Makefile:
#
#   $(shell ...)   not a thing in BSD make, and its absence is deliberate
#   += and ?=     ?= is portable and used below; += is not relied on
#   ifeq/else     GNU make conditionals, parsed as target names by BSD make
#   .for          BSD make loops, which GNU make does not have
#
# Where a repetition is needed, the recipe uses a shell loop, which both makes
# run the same way. Nothing is computed by make and nothing is computed by the
# shell that make cannot also do, so both of them produce the same build.
#
# Every artifact is written under build/. That directory also holds worktrees,
# so `make clean` removes build products by name and never the directory.

GO ?= go
MODULE := github.com/glenjbarber/orcli

BIN := build/orcli
MAIN := ./cmd/orcli

# Every target that is compiled and vetted by `make crossbuild`. The terminal
# layer names ioctl requests that differ between the BSD family and System V,
# and nothing else would notice a change that breaks a platform other than the
# one running the tests.
#
# DragonFly is absent because the SQLite driver cannot be built for it, and the
# target was dropped rather than shipped with a command reporting that it has no
# driver. Windows is absent because the bootstrap loader compares devices
# through syscall.Stat_t.
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 \
             freebsd/amd64 freebsd/arm64 openbsd/amd64 netbsd/amd64

# CGO is off for every cross build. A cross target has no C toolchain on this
# host, and a driver that compiles C would produce a binary that builds for
# every target and then fails at the first query.
export CGO_ENABLED = 0

.PHONY: all build test check cover lint vet fmt fmtwrite crossbuild tidy clean help

all: build

# build places the binary at build/orcli.
#
# A missing main package is reported and the target succeeds. It is an ordinary
# message rather than a refusal: the library packages still compile, and a build
# that refuses because a package has not been written yet stops the whole tree
# from being built at all.
build:
	@mkdir -p build
	@if [ -f cmd/orcli/main.go ]; then \
		$(GO) build -o $(BIN) $(MAIN); \
	else \
		echo "no main package yet, so nothing is placed at $(BIN)"; \
	fi

test:
	$(GO) test ./...

# check runs the suite under the race detector. CGO is re-enabled for it,
# because the detector is not available without it.
check:
	@mkdir -p build
	CGO_ENABLED=1 $(GO) test -race ./...

cover:
	@mkdir -p build
	CGO_ENABLED=1 $(GO) test -coverprofile=build/coverage.out ./...
	$(GO) tool cover -func=build/coverage.out

# lint is the gate CI runs. It is a check and nothing more: no target in it
# edits a file, because a lint step in CI that rewrites a tree reports success
# while the problem it was asked to find is still there.
lint: fmt vet

# fmt fails if gofmt would change any file. It asks gofmt for a list and
# requires the list to be empty. `go fmt` is not used here, because `go fmt`
# rewrites the files it visits and a check that edits is not a check.
fmt:
	@out=$$(gofmt -l . 2>&1); \
	if [ -n "$$out" ]; then \
		echo "gofmt reported differences:"; \
		echo "$$out"; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

# fmtwrite is the correcting form, kept separate so that lint never edits a tree
# a reader did not ask it to edit.
fmtwrite:
	gofmt -w .

# crossbuild compiles and vets every supported target. Vet runs under each
# GOOS as well, since a type error can be specific to one platform.
crossbuild:
	@for target in $(PLATFORMS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "==> $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch $(GO) build ./... || exit 1; \
		GOOS=$$os GOARCH=$$arch $(GO) vet ./... || exit 1; \
		if [ -f cmd/orcli/main.go ]; then \
			GOOS=$$os GOARCH=$$arch $(GO) build \
				-o build/$$os-$$arch/orcli $(MAIN) || exit 1; \
		fi; \
	done
	@echo "crossbuild: every target compiled and vetted"

tidy:
	$(GO) mod tidy

# clean removes build products by name. It does not remove build/ itself, or
# anything else under it, because build/ also holds worktrees.
clean:
	rm -f $(BIN) build/coverage.out
	@for target in $(PLATFORMS); do \
		rm -f build/$${target%/*}-$${target#*/}/orcli; \
	done

help:
	@echo "all        build the binary into build/"
	@echo "build      build the binary into build/orcli"
	@echo "test       run the suite"
	@echo "check      run the suite under the race detector"
	@echo "cover      run the suite and write build/coverage.out"
	@echo "lint       gofmt check and go vet"
	@echo "vet        go vet"
	@echo "fmt        fail if gofmt would change a file"
	@echo "fmtwrite   rewrite files with gofmt"
	@echo "crossbuild compile and vet every supported target"
	@echo "tidy       tidy go.mod"
	@echo "clean      remove build products, keeping build/ itself"
