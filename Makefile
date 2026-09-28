# gnopie. `make help` lists what is here.
GNOROOT ?= $(HOME)/p/gh/gnolang/gno

.PHONY: help
help:
	@echo "install   build and install gnopie into GOBIN"
	@echo "build     build ./build/gnopie"
	@echo "test      run the suite (needs GNOROOT, see README)"
	@echo "lint      go vet"
	@echo "tidy      go mod tidy"
	@echo "GNOROOT is currently $(GNOROOT)"

.PHONY: install
install:
	go install .

.PHONY: build
build:
	go build -o build/gnopie .

# The suite spins an in-memory gno node and loads realms out of a gno source
# tree, so it needs one on disk and the tree has to match the gno the module is
# pinned to. A mismatch does not fail cleanly: it surfaces as
# `invalid gno package; type check failed`, which says nothing about versions.
.PHONY: test
test:
	@test -d "$(GNOROOT)/examples" || { \
	  echo "GNOROOT=$(GNOROOT) has no examples/. Point it at a gnolang/gno checkout"; \
	  echo "at the tag this module pins (see README, Testing)."; exit 1; }
	GNOROOT=$(GNOROOT) go test ./...

.PHONY: lint
lint:
	go vet ./...

.PHONY: tidy
tidy:
	go mod tidy
