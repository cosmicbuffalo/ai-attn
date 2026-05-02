package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// withTempHome sets up an isolated temp HOME directory for the test and returns its path.
// Pins GOMODCACHE to a stable location *outside* the temp HOME — otherwise go-build
// subprocesses would populate $HOME/go/pkg/mod inside the temp dir, and TempDir's
// RemoveAll cleanup would trip over the read-only files Go writes into the module cache.
func withTempHome(t *testing.T) string {
	t.Helper()
	modCache := stableGOMODCACHE()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", os.Getenv("PATH"))
	t.Setenv("TMUX_PANE", "")
	t.Setenv("AI_ATTN_CONFIG", "")
	t.Setenv("AI_ATTN_STATE_DIR", "")
	if modCache != "" {
		t.Setenv("GOMODCACHE", modCache)
	}
	return home
}

// stableGOMODCACHE returns a path that go-build subprocesses can use as their module
// cache regardless of HOME being overridden. Honors GOMODCACHE if set, otherwise GOPATH,
// otherwise falls back to <real-HOME>/go/pkg/mod, otherwise empty (caller skips the override).
// Must be called before HOME is overridden so os.UserHomeDir resolves to the real HOME.
func stableGOMODCACHE() string {
	if v := os.Getenv("GOMODCACHE"); v != "" {
		return v
	}
	if v := os.Getenv("GOPATH"); v != "" {
		return filepath.Join(v, "pkg", "mod")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "go", "pkg", "mod")
	}
	return ""
}

// runCLI invokes run() in-process with the given args and returns (exit code, stdout, stderr).
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rc := run(args, strings.NewReader(""), &stdout, &stderr)
	return rc, stdout.String(), stderr.String()
}

// buildBinary compiles the ai-attn binary into a temp directory and returns its path.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ai-attn")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/ai-attn")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "go-build-cache"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, string(out))
	}
	return bin
}

// repoRoot returns the absolute path to the repository root (two levels up from cmd/ai-attn).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
