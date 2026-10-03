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
# Two directories, kept apart on purpose, and neither of them is the source
# tree:
#
#   staged/bin/  build output. It holds the compiled binary, the per-target
#                binaries, and the coverage profile, and nothing else, so
#                `make clean` removes the whole of it rather than a list of
#                files that has to be kept in step with whatever the build
#                starts writing.
#   worktrees/   a checkout of this repository per branch. Nothing here is ever
#                written by a build and nothing here is ever touched by
#                `make clean`.
#
# They are separate so that `make clean` cannot reach a worktree. Cleaning by
# name in a directory that also holds a checkout is a target list that grows a
# hole every time something new is written there, and the hole is a worktree
# that gets deleted instead of a build product.
#
# `staged/` rather than `bin/` at the top, because a checkout is not somewhere
# a build product belongs. Putting the output one level down names it as what it
# is, and leaves room beside it for something a future target stages there.

GO ?= go
MODULE := github.com/glenjbarber/orcli

# Output lives under staged/bin/. The binary is named for the program, so an
# install target has one file to place rather than a directory to copy.
STAGE := staged
BINDIR := $(STAGE)/bin
BIN := $(BINDIR)/orcli
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
# host, and a driver that compiles C would produce a binary that builds for every
# target and then fails at the first query.
export CGO_ENABLED = 0

.PHONY: all build test check cover lint vet fmt fmtwrite crossbuild tidy \
        prune-merged clean help

all: build

# build places the binary at staged/bin/orcli.
#
# A missing main package is reported and the target succeeds. It is an ordinary
# message rather than a refusal: the library packages still compile, and a build
# that refuses because a package has not been written yet stops the whole tree
# from being built at all.
build:
	@mkdir -p $(BINDIR)
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
	CGO_ENABLED=1 $(GO) test -race ./...

cover:
	@mkdir -p $(BINDIR)
	CGO_ENABLED=1 $(GO) test -coverprofile=$(BINDIR)/coverage.out ./...
	$(GO) tool cover -func=$(BINDIR)/coverage.out

# lint is the gate CI runs. It is a check and nothing more: no target in it
# edits a file, because a lint step in CI that rewrites a tree reports success
# while the problem it was asked to find is still there.
lint: fmt vet

# PKGDIRS lists the directories holding a package of this module.
#
# gofmt is pointed at these rather than at `.`, because `.` is a filesystem walk
# and worktrees/ holds checkouts. A worktree holds a copy of the module, so a
# walk from the top level finds every file in it, and an unformatted draft in
# another worktree fails the gate on this branch. Asking the module for its own
# packages cannot reach a directory the module does not contain.
#
# The module prefix is stripped so the paths are relative and so the output is
# the same on a machine where the checkout is not under GOPATH. A module with no
# packages prints nothing but warnings, so the recipe checks the output rather
# than the exit status, which is sed's.
PKGDIRS = $(GO) list -f '{{.Dir}}' ./... 2>/dev/null | sed "s|$(PWD)/||; s|^$(MODULE)$$|.|"

# fmt fails if gofmt would change any file in a package of this module. It asks
# gofmt for a list and requires the list to be empty. `go fmt` is not used here,
# because `go fmt` rewrites the files it visits and a check that edits is not a
# check.
fmt:
	@dirs=$$($(PKGDIRS)); \
	if [ -z "$$dirs" ]; then \
		echo "no packages to format yet"; \
	elif out=$$(gofmt -l $$dirs 2>&1) && [ -n "$$out" ]; then \
		echo "gofmt reported differences:"; \
		echo "$$out"; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

# fmtwrite is the correcting form, kept separate so that lint never edits a tree
# a reader did not ask it to edit.
fmtwrite:
	@dirs=$$($(PKGDIRS)); \
	if [ -n "$$dirs" ]; then \
		gofmt -w $$dirs; \
	fi

# crossbuild compiles and vets every supported target. Vet runs under each GOOS
# as well, since a type error can be specific to one platform.
#
# Each target's binary goes to its own directory under staged/bin/, so two
# targets cannot overwrite each other's output and a stale artifact is not
# mistaken for a current one.
crossbuild:
	@for target in $(PLATFORMS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "==> $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch $(GO) build ./... || exit 1; \
		GOOS=$$os GOARCH=$$arch $(GO) vet ./... || exit 1; \
		if [ -f cmd/orcli/main.go ]; then \
			mkdir -p $(BINDIR)/$$os-$$arch; \
			GOOS=$$os GOARCH=$$arch $(GO) build \
				-o $(BINDIR)/$$os-$$arch/orcli $(MAIN) || exit 1; \
		fi; \
	done
	@echo "crossbuild: every target compiled and vetted"

tidy:
	$(GO) mod tidy

# prune-merged removes the worktrees and branches whose work is on the main
# line, and touches nothing else.
#
# Every spawned worker lands on a branch and every one of them is merged
# eventually, so the list of branches grows without bound and every one of them
# is something `git branch` lists and a reader has to read past. A target that
# clears them is the check happening every time rather than once.
#
# Two refusals, and both are the point of the target:
#
#   - a branch that is not an ancestor of main is left alone. Its work may be
#     unmerged, or uncommitted, or both, and `git branch -d` would refuse it
#     anyway; asking first and saying so is clearer than a failure per branch.
#   - a worktree with a modified or untracked file is left alone, along with the
#     branch checked out in it. A worker between steps looks exactly like this,
#     and removing the directory would destroy work that exists nowhere else.
#
# Nothing is forced. `-d` rather than `-D`, and no `--force` on the worktree, so
# a branch that somehow is not merged and a directory that somehow is not clean
# are both left in place rather than deleted on the strength of this recipe
# being wrong about them.
#
# PRUNE_KEEP is a space-separated list of branch names to leave alone, for a
# branch a reader wants to keep a checkout for.
PRUNE_KEEP ?=

prune-merged:
	@kept=0; removed=0; \
	for branch in `git branch --format='%(refname:short)'`; do \
		case " $$PRUNE_KEEP " in \
			*" $$branch "*) echo "keeping $$branch, it is named in PRUNE_KEEP"; \
				kept=$$((kept+1)); continue;; \
		esac; \
		[ "$$branch" = "main" ] && continue; \
		if git merge-base --is-ancestor "$$branch" main; then \
			merged=yes; \
		else \
			merged=no; \
			echo "leaving $$branch, its work is not on the main line"; \
			kept=$$((kept+1)); \
			continue; \
		fi; \
		for wt in `git worktree list --porcelain | sed -n 's/^worktree //p'`; do \
			if [ "`git -C "$$wt" rev-parse --abbrev-ref HEAD 2>/dev/null`" != "$$branch" ]; then \
				continue; \
			fi; \
			if [ -n "`git -C "$$wt" status --short 2>/dev/null`" ]; then \
				echo "leaving $$branch, $$wt has work in it"; \
				kept=$$((kept+1)); \
				merged=no; \
				continue; \
			fi; \
			git worktree remove "$$wt" || exit 1; \
			echo "removed the worktree $$wt"; \
		done; \
		[ "$$merged" = "no" ] && continue; \
		git branch -d "$$branch" || exit 1; \
		removed=$$((removed+1)); \
	done; \
	git worktree prune; \
	echo "prune-merged: $$removed removed, $$kept left alone"

# clean removes staged/bin/ and nothing else.
#
# The directory is removed whole rather than by name, because it holds build
# products and nothing else. worktrees/ is not touched at all: it holds
# checkouts, and a clean that reached one would delete a branch rather than a
# build product.
clean:
	rm -rf $(BINDIR)

help:
	@echo "all          build the binary into staged/bin/"
	@echo "build        build the binary into staged/bin/orcli"
	@echo "test         run the suite"
	@echo "check        run the suite under the race detector"
	@echo "cover        run the suite and write staged/bin/coverage.out"
	@echo "lint         gofmt check and go vet"
	@echo "vet          go vet"
	@echo "fmt          fail if gofmt would change a package of this module"
	@echo "fmtwrite     rewrite the packages of this module with gofmt"
	@echo "crossbuild   compile and vet every supported target"
	@echo "tidy         tidy go.mod"
	@echo "prune-merged remove worktrees and branches already on the main line"
	@echo "clean        remove staged/bin/, which holds build products and nothing else"