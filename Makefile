# gnopie. `make help` lists what is here.
#
# No `build` target on purpose: `go build` is already one word, and a target that
# only wraps it is a place for the flags to drift apart from CI's.
#
# One variable matters: GNOROOT, a gnolang/gno checkout AT THE TAG go.mod PINS.
# gnopie links gno as a library, and the tests load realms out of that tree and
# type-check them with the linked VM, so a mismatched tree fails as
# `invalid gno package; type check failed`, which names neither the version nor
# the line. `make gnoroot` prints the command that produces the right one.

GO            ?= go
GNOROOT       ?= $(HOME)/p/gh/gnolang/gno
STATICCHECK_VERSION ?= v0.7.0
ARGS          ?= --help

# The gno version this module pins, read from go.mod rather than written down
# twice. Everything that needs to know the tag asks here.
GNO_VERSION = $(shell $(GO) list -m -f '{{.Version}}' github.com/gnolang/gno)

.DEFAULT_GOAL := help

.PHONY: help
help: ## show this help
	@awk 'BEGIN{FS=":.*?## "} /^[a-z][a-z-]*:.*?## /{printf "  \033[36m%-12s\033[0m %s\n",$$1,$$2}' $(MAKEFILE_LIST)
	@echo ""
	@echo "  GNOROOT      $(GNOROOT)"
	@echo "  gno pinned   $(GNO_VERSION)"

.PHONY: all
all: lint test ## what CI runs, which is what you want before pushing

.PHONY: test
test: ## the full suite, including the tests that boot a chain
	@test -d "$(GNOROOT)/examples" || { \
	  echo "GNOROOT=$(GNOROOT) has no examples/."; \
	  echo "The chain tests will SKIP. Run 'make gnoroot' for the fix."; }
	GNOROOT=$(GNOROOT) $(GO) test ./...

.PHONY: test-unit
test-unit: ## the pure tests only: no chain, no GNOROOT, well under a second
	$(GO) test -short ./...

.PHONY: lint
lint: ## gofmt, vet, and staticcheck when it is on PATH
	@out=$$(gofmt -l . ); if [ -n "$$out" ]; then echo "gofmt:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then \
	  staticcheck ./...; \
	else \
	  echo "staticcheck not on PATH, skipping it."; \
	  echo "  go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)"; \
	fi

.PHONY: fmt
fmt: ## rewrite everything gofmt would change
	gofmt -w .

.PHONY: install
install: ## put gnopie on your PATH
	$(GO) install .

.PHONY: run
run: ## run it: make run ARGS="INSPECT gno.land/r/gnoland/blog"
	$(GO) run . $(ARGS)

.PHONY: devnode
devnode: ## boot a throwaway chain and print where it is
	GNOROOT=$(GNOROOT) $(GO) run ./scripts/devnode -home /tmp/gnopie-devhome

.PHONY: screenshots
screenshots: ## regenerate docs/img/ from real command output
	GNOROOT=$(GNOROOT) ./scripts/screenshots.sh

.PHONY: gnoroot
gnoroot: ## print how to get a gno checkout at the pinned tag
	@echo "gnopie pins gno $(GNO_VERSION). A worktree at that tag, from a checkout you already have:"
	@echo ""
	@echo "  git -C /path/to/gnolang/gno worktree add /tmp/gno-$(GNO_VERSION) $(GNO_VERSION)"
	@echo "  GNOROOT=/tmp/gno-$(GNO_VERSION) make test"
	@echo ""
	@echo "or from nothing:"
	@echo ""
	@echo "  git clone --depth 1 --branch $(GNO_VERSION) https://github.com/gnolang/gno /tmp/gno-$(GNO_VERSION)"

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy
