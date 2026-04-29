.PHONY: help build install dev-setup test test-clean clean completion-bash completion-zsh \
        build-all build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64

BIN := mega-mem
REPO_TEMPLATES := $(CURDIR)/share/mega-mem/templates/default
XDG_TEMPLATES  := $(HOME)/.local/share/mega-mem/templates
TEST_ALIAS     := mm-test

# Supported release platforms — mirrors FEATURES.md v1 distribution list.
PLATFORMS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64

help:
	@echo "Targets:"
	@echo "  dev-setup    One-time: symlink repo templates into ~/.local/share/mega-mem/templates/"
	@echo "  install      go install ./cmd/mega-mem (rebuilds + puts binary on PATH via GOBIN)"
	@echo "  build        Native build into ./bin/mega-mem"
	@echo "  build-all    Cross-compile every supported platform into ./bin/"
	@echo "  build-<plat> Cross-compile a single platform (linux-amd64, linux-arm64,"
	@echo "               darwin-amd64, darwin-arm64)"
	@echo "  test         End-to-end smoke test using alias '$(TEST_ALIAS)'"
	@echo "  test-clean   Remove the test alias and its vault directory"
	@echo "  clean        Remove ./bin/ and Go build cache"
	@echo "  completion-bash / completion-zsh  Generate shell completion to stdout"

# ----- dev setup -----

dev-setup:
	@mkdir -p $(dir $(XDG_TEMPLATES))
	@if [ -L $(XDG_TEMPLATES) ] || [ -d $(XDG_TEMPLATES) ]; then \
		echo "Removing existing $(XDG_TEMPLATES)"; \
		rm -rf $(XDG_TEMPLATES); \
	fi
	@ln -s $(REPO_TEMPLATES) $(XDG_TEMPLATES)
	@echo "Symlinked: $(XDG_TEMPLATES) -> $(REPO_TEMPLATES)"
	@echo ""
	@echo "One-time reminders:"
	@echo "  - Ensure \$$HOME/go/bin is on PATH (add to ~/.bashrc if not already)"
	@echo "  - Then 'make install' to build and install the binary"

# ----- build / install -----

install:
	go install ./cmd/mega-mem

build:
	@mkdir -p bin
	go build -o bin/$(BIN) ./cmd/mega-mem

# Cross-compile a single platform: `make build-linux-amd64`, etc.
# The $* is the pattern-match suffix (e.g. "linux-amd64"), which we split into
# $(GOOS)/$(GOARCH) via the first/second word of a substituted string.
build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64:
	@mkdir -p bin
	$(eval OS := $(word 1,$(subst -, ,$(subst build-,,$@))))
	$(eval ARCH := $(word 2,$(subst -, ,$(subst build-,,$@))))
	@echo "building $(OS)/$(ARCH) -> bin/$(BIN)-$(OS)-$(ARCH)"
	@GOOS=$(OS) GOARCH=$(ARCH) go build -o bin/$(BIN)-$(OS)-$(ARCH) ./cmd/mega-mem

# Build every supported platform. Depends on each individual target so they
# run in parallel under `make -j`.
build-all: $(addprefix build-,$(PLATFORMS))
	@echo ""
	@echo "Artifacts in bin/:"
	@ls -1 bin/ | grep '^$(BIN)-' | sed 's/^/  /'

# ----- smoke test -----

test:
	@echo "=== register $(TEST_ALIAS) (default path) ==="
	$(BIN) vaults register $(TEST_ALIAS) --force
	@echo ""
	@echo "=== init ==="
	$(BIN) vault $(TEST_ALIAS) init
	@echo ""
	@echo "=== status ==="
	$(BIN) vault $(TEST_ALIAS) status
	@echo ""
	@echo "=== scaffold org at orgs/example ==="
	$(BIN) vault $(TEST_ALIAS) scaffold org orgs/example
	@echo ""
	@echo "=== idempotent scaffold (should be no-op) ==="
	$(BIN) vault $(TEST_ALIAS) scaffold
	@echo ""
	@echo "=== vaults check ==="
	$(BIN) vaults check $(TEST_ALIAS) --drift
	@echo ""
	@echo "=== template path ==="
	$(BIN) template path
	@echo ""
	@echo "All smoke tests passed. Run 'make test-clean' to tear down."

test-clean:
	-$(BIN) vaults unregister $(TEST_ALIAS)
	rm -rf $(HOME)/.local/share/mega-mem/vaults/$(TEST_ALIAS)

# ----- cleanup -----

clean:
	rm -rf bin/
	go clean

# ----- shell completion (optional) -----

completion-bash:
	@$(BIN) completion bash

completion-zsh:
	@$(BIN) completion zsh
