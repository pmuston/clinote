# clinote — local build and check targets.
#
# `make check` is what CI's build job runs. `make linux` runs the same thing inside
# a Linux container, which is worth doing before pushing: the shell tests assert
# against every shell in supportedShells and refuse to skip a missing one, so a
# macOS pass can still fail on CI where zsh is absent.

GO      ?= go
BIN     ?= clinote
DIST    ?= dist
GOIMAGE ?= golang:1.25

# Container runtime: Apple's native `container` (macOS 26+) is preferred when
# present, else docker. Override with RUNTIME=docker.
RUNTIME ?= $(shell command -v container 2>/dev/null || command -v docker 2>/dev/null)

.DEFAULT_GOAL := check
.PHONY: help check build vet fmt test race conformance linux clean

help: ## List targets
	@grep -hE '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*##/\t/' | expand -t22

check: build vet fmt test ## Build, vet, gofmt and test — CI's build job

build: ## Compile everything
	$(GO) build ./...

vet: ## go vet
	$(GO) vet ./...

fmt: ## Fail if anything is not gofmt'd
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "not gofmt'd:"; echo "$$unformatted"; exit 1; fi

test: ## Run tests with the race detector
	$(GO) test -race -timeout 5m ./...

race: test ## Alias for test

# The contract between clinote and notekit is conformance, not shared code: this
# makes clinote write notebooks and has notekit's own checker judge them. A
# notebook the kit cannot read must fail here rather than in someone's editor.
conformance: ## Migrate the v1 corpus and check it with notekit's notefmt
	@set -e; \
	tmp=$$(mktemp -d); \
	$(GO) build -o $$tmp/$(BIN) ./cmd/clinote; \
	cp internal/migrate/testdata/v1/*.md $$tmp/; \
	cd $$tmp; \
	for f in *.md; do ./$(BIN) migrate "$$f" >/dev/null; done; \
	$(GO) run github.com/pmuston/notekit/cmd/notefmt@latest -strict check *.v2.md; \
	echo "conformance ok ($$(ls *.v2.md | wc -l | tr -d ' ') notebooks)"; \
	rm -rf $$tmp

# Runs `check` on Linux. zsh is installed because the shell tests refuse to skip a
# shell they cannot find — which is exactly the failure a macOS-only run misses.
linux: ## Run check inside a Linux container
	@if [ -z "$(RUNTIME)" ]; then \
		echo "no container runtime found."; \
		echo "  install Apple's native containers (macOS 26+), or start Docker Desktop,"; \
		echo "  or run: make check"; \
		exit 1; \
	fi
	@echo "==> $(notdir $(RUNTIME)) / $(GOIMAGE)"
	$(RUNTIME) run --rm -v "$(CURDIR)":/src -w /src $(GOIMAGE) \
		sh -c 'apt-get update -qq && apt-get install -y -qq zsh >/dev/null && \
		       go build ./... && go vet ./... && go test -race -timeout 5m ./...'

clean: ## Remove build output
	rm -rf $(DIST) $(BIN)
