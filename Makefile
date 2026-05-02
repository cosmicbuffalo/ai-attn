.PHONY: build test format lint check install uninstall clean install-hooks

BIN := ai-attn
CMD := ./cmd/ai-attn
INSTALL_DIR := $(HOME)/.local/share/ai-attn
BIN_DIR := $(HOME)/.local/bin

build:
	go build -ldflags "-s -w" -o $(BIN) $(CMD)

test:
	go test ./...

format:
	gofmt -w .

lint:
	go vet ./...

check:
	@bash scripts/check.sh

install-hooks:
	@hooks_dir="$$(git rev-parse --git-path hooks)"; \
	mkdir -p "$$hooks_dir"; \
	{ \
		echo '#!/usr/bin/env bash'; \
		echo 'exec bash "$$(git rev-parse --show-toplevel)/scripts/pre-push.sh" "$$@"'; \
	} > "$$hooks_dir/pre-push"; \
	chmod +x "$$hooks_dir/pre-push"; \
	echo "Installed pre-push hook at $$hooks_dir/pre-push"; \
	echo "Hook runs the check suite when pushing main; bypass with: git push --no-verify"

install: build
	mkdir -p $(INSTALL_DIR)/bin $(INSTALL_DIR)/hooks $(BIN_DIR)
	cp $(BIN) $(INSTALL_DIR)/bin/$(BIN)
	ln -sf $(INSTALL_DIR)/bin/$(BIN) $(BIN_DIR)/$(BIN)
	install -m 0755 hooks/_common.sh $(INSTALL_DIR)/hooks/_common.sh
	install -m 0755 hooks/claude.sh $(INSTALL_DIR)/hooks/claude.sh
	install -m 0755 hooks/codex.sh $(INSTALL_DIR)/hooks/codex.sh
	install -m 0755 hooks/opencode.sh $(INSTALL_DIR)/hooks/opencode.sh
	install -m 0755 uninstall.sh $(INSTALL_DIR)/uninstall.sh
	@echo ""
	@echo "Installed ai-attn."
	@echo ""
	@echo "Binary:"
	@echo "  $(BIN_DIR)/$(BIN)"
	@echo ""
	@echo "Next step — wire hooks into your AI agent config:"
	@echo "  Ask your AI agent to read AGENTS.md in this repo and follow the instructions."
	@echo ""
	@echo "  Or wire hooks manually — see README.md for details."
	@echo ""
	@echo "Run diagnostics:"
	@echo "  ai-attn doctor"
	@case ":$$PATH:" in \
		*":$(BIN_DIR):"*) ;; \
		*) \
			echo ""; \
			echo "NOTE: $(BIN_DIR) is not in your PATH."; \
			echo "Add to your shell profile (~/.bashrc, ~/.zshrc, etc.):"; \
			echo ""; \
			echo "  export PATH=\"$(BIN_DIR):\$$PATH\""; \
			;; \
	esac

uninstall:
	bash uninstall.sh

clean:
	rm -f $(BIN)
