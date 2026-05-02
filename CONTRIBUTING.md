# Contributing to ai-attn

Thanks for your interest in contributing! This is a small, focused tool and contributions of all sizes are welcome.

## Getting Started

```bash
git clone https://github.com/cosmicbuffalo/ai-attn.git
cd ai-attn
make test
```

The project keeps dependencies minimal: the Go standard library plus `github.com/BurntSushi/toml` for parsing the user's config file.

## Development

A Makefile is provided for common tasks:

```bash
make build          # build the binary locally
make test           # run all tests
make format         # format Go source with gofmt
make lint           # run go vet
make check          # run the full pre-push check suite (gofmt, vet, test, shellcheck) — same as CI
make install        # build from source and install to ~/.local
make install-hooks  # install a git pre-push hook that runs `make check` when pushing main (recommended)
make uninstall      # remove installed binary, hooks, and symlink
make clean          # remove locally built binary
```

`make install` builds from source and installs the binary, hooks, and symlink — use it to test your changes end-to-end.

It's recommended to run `make install-hooks` after cloning so failing checks are caught locally before they reach CI. The hook only runs when the push includes `main`; feature-branch pushes are unaffected. Bypass any single push with `git push --no-verify` if needed.

You can also run tests directly with extra flags:

```bash
go test ./... -v             # verbose output
go test ./... -run TestHook  # run specific tests
```

The test suite includes unit tests and integration tests that build the binary and exercise the actual hook scripts. Integration tests take a few seconds due to the build step.

### Shell Linting

```bash
shellcheck hooks/*.sh install.sh uninstall.sh
```

CI runs all of these on every push and pull request.

## Project Structure

```
cmd/ai-attn/
  main.go              # entrypoint and command dispatch
  hooks.go             # hook handlers for Claude, Codex, OpenCode (cmdHook + per-agent dispatch)
  record_write.go      # session-key derivation and state-file I/O
  commands_state.go    # set-state, clear-state, status, list, gc, clear
  commands_admin.go    # doctor, test, logs, init-config
  config.go            # config loading, paths, TOML file helpers
  types.go             # Record, Config, sessionIdentity, exit codes
  format.go            # terminal formatting helpers
  *_test.go            # tests for each module
hooks/
  _common.sh           # shared hook preamble (env, log rotation, binary check)
  claude.sh            # Claude Code hook script
  codex.sh             # Codex hook script
  opencode.sh          # OpenCode hook script
plugins/opencode/      # OpenCode Bun plugin (index.mjs)
install.sh             # installer (downloads binary or builds from source)
uninstall.sh           # uninstaller
Makefile               # development commands (build, test, install, etc.)
```

## Making Changes

1. Fork and clone the repo
2. Create a branch (`git checkout -b my-change`)
3. Make your changes
4. Run `make check` to verify
5. Commit with a clear message describing the "why"
6. Open a pull request

## Adding Support for a New AI Agent

To add support for a new agent:

1. Add a `hookNewAgent()` function in `hooks.go` that parses the agent's event format and calls `writeHookState()`
2. Add a case for the new agent in the `cmdHook()` dispatcher (also in `hooks.go`)
3. Create a hook script in `hooks/` (use `hooks/_common.sh` for shared preamble)
4. Add tests in `hooks_test.go`
5. Document the hook wiring instructions in `AGENTS.md` so AI agents can wire the new hook into the user's config, and link to it from the README's hooks section

## Design Principles

- **Minimal dependencies.** Currently only `github.com/BurntSushi/toml` (for config parsing). Don't add new dependencies without a strong reason — prefer the standard library.
- **Hooks must never break the parent agent.** Hook scripts always `exit 0`, even on errors. Errors go to the log file.
- **Simple state writes.** State files are small (< 1 KB) and written via `os.WriteFile`. Last-write-wins semantics are acceptable since downstream consumers poll periodically and only care about the most recent value.
- **Minimal surface area.** Resist adding features that aren't directly needed. Simplicity is a feature.

## Reporting Issues

Open an issue at https://github.com/cosmicbuffalo/ai-attn/issues with:

- What you expected to happen
- What actually happened
- Output of `ai-attn doctor`
- Your OS and architecture
