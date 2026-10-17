.PHONY: build test format lint check install uninstall clean install-hooks

BIN := ai-attn
CMD := ./cmd/ai-attn
VERSION := $(shell tr -d '[:space:]' < VERSION)
INSTALL_DIR := $(HOME)/.local/share/ai-attn
BIN_DIR := $(HOME)/.local/bin

build:
	go build -ldflags "-s -w -X main.version=v$(VERSION)" -o $(BIN) $(CMD)

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
	mkdir -p $(INSTALL_DIR)/plugins/opencode $(INSTALL_DIR)/plugins/muse/.muse-plugin $(INSTALL_DIR)/plugins/muse/hooks
	cp $(BIN) $(INSTALL_DIR)/bin/$(BIN)
	ln -sf $(INSTALL_DIR)/bin/$(BIN) $(BIN_DIR)/$(BIN)
	install -m 0755 hooks/_common.sh $(INSTALL_DIR)/hooks/_common.sh
	install -m 0755 hooks/claude.sh $(INSTALL_DIR)/hooks/claude.sh
	install -m 0755 hooks/codex.sh $(INSTALL_DIR)/hooks/codex.sh
	install -m 0755 hooks/opencode.sh $(INSTALL_DIR)/hooks/opencode.sh
	install -m 0644 plugins/opencode/index.mjs $(INSTALL_DIR)/plugins/opencode/index.mjs
	install -m 0644 plugins/opencode/package.json $(INSTALL_DIR)/plugins/opencode/package.json
	install -m 0644 plugins/muse/.muse-plugin/plugin.json $(INSTALL_DIR)/plugins/muse/.muse-plugin/plugin.json
	install -m 0755 plugins/muse/hooks/_common.sh $(INSTALL_DIR)/plugins/muse/hooks/_common.sh
	install -m 0755 plugins/muse/hooks/user_prompt_submit.sh $(INSTALL_DIR)/plugins/muse/hooks/user_prompt_submit.sh
	install -m 0755 plugins/muse/hooks/permission_request.sh $(INSTALL_DIR)/plugins/muse/hooks/permission_request.sh
	install -m 0755 plugins/muse/hooks/notification.sh $(INSTALL_DIR)/plugins/muse/hooks/notification.sh
	install -m 0755 plugins/muse/hooks/post_tool_use.sh $(INSTALL_DIR)/plugins/muse/hooks/post_tool_use.sh
	install -m 0755 plugins/muse/hooks/post_tool_use_failure.sh $(INSTALL_DIR)/plugins/muse/hooks/post_tool_use_failure.sh
	install -m 0755 plugins/muse/hooks/stop.sh $(INSTALL_DIR)/plugins/muse/hooks/stop.sh
	install -m 0755 plugins/muse/hooks/stop_failure.sh $(INSTALL_DIR)/plugins/muse/hooks/stop_failure.sh
	install -m 0755 plugins/muse/hooks/session_end.sh $(INSTALL_DIR)/plugins/muse/hooks/session_end.sh
	install -m 0755 uninstall.sh $(INSTALL_DIR)/uninstall.sh
	@echo ""
	@echo "Installed ai-attn."
	@echo ""
	@echo "Binary:"
	@echo "  $(BIN_DIR)/$(BIN)"
	@echo ""
	@echo "Wiring hooks..."
	@"$(BIN_DIR)/$(BIN)" setup || true
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
