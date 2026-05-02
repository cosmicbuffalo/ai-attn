package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitConfigCreatesFile verifies that init-config creates a valid default config TOML file
// and that loading it yields the documented defaults.
func TestInitConfigCreatesFile(t *testing.T) {
	home := withTempHome(t)
	rc, _, _ := runCLI(t, "init-config")
	if rc != exitOK {
		t.Fatalf("init-config rc=%d", rc)
	}

	cfg := filepath.Join(home, ".config", "ai-attn", "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := parseConfig(data)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if !parsed.Enabled {
		t.Fatalf("expected enabled=true, got %+v", parsed)
	}
	if parsed.TTLSeconds != 72*3600 {
		t.Fatalf("expected ttl_seconds=259200, got %+v", parsed)
	}
}

// TestDoctorReportsHealth verifies that doctor exits 0 and reports all checks passed when hooks are wired.
func TestDoctorReportsHealth(t *testing.T) {
	home := withTempHome(t)
	hookDir := filepath.Join(home, ".local", "share", "ai-attn", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{"claude.sh", "codex.sh", "opencode.sh"} {
		if err := os.WriteFile(filepath.Join(hookDir, hook), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"hooks":{"Notification":[{"matcher":"","hooks":[{"type":"command","command":"bash ~/.local/share/ai-attn/hooks/claude.sh","timeout":10,"async":true}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(`notify = ["bash", "/tmp/x/.local/share/ai-attn/hooks/codex.sh"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	opencodeDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opencodeDir, "opencode.jsonc"), []byte(`{"plugin":["/tmp/x/.local/share/ai-attn/plugins/opencode"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, stdout, _ := runCLI(t, "doctor")
	if rc != exitOK {
		t.Fatalf("doctor rc=%d", rc)
	}
	if !strings.Contains(stdout, "ai-attn") {
		t.Fatalf("expected version in output: %s", stdout)
	}
	for _, field := range []string{"config_status=", "state_dir_status=ok", "enabled=true", "ttl_seconds=259200", "All checks passed."} {
		if !strings.Contains(stdout, field) {
			t.Fatalf("expected %q in doctor output: %s", field, stdout)
		}
	}
}

// TestDoctorFailsWhenHooksNotWired verifies that doctor exits 1 when hook scripts are missing.
func TestDoctorFailsWhenHooksNotWired(t *testing.T) {
	withTempHome(t)
	rc, stdout, _ := runCLI(t, "doctor")
	if rc != exitError {
		t.Fatalf("expected doctor failure, rc=%d output=%s", rc, stdout)
	}
	for _, field := range []string{"hook_claude=missing", "hook_codex=missing", "Some checks failed."} {
		if !strings.Contains(stdout, field) {
			t.Fatalf("expected %q in doctor output: %s", field, stdout)
		}
	}
}

// TestDoctorFailsOnSemanticallyInvalidConfig verifies that doctor detects an invalid config value (e.g., wrong type).
func TestDoctorFailsOnSemanticallyInvalidConfig(t *testing.T) {
	home := withTempHome(t)
	cfg := filepath.Join(home, ".config", "ai-attn", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`ttl_seconds = "bad"`), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, stdout, _ := runCLI(t, "doctor")
	if rc != exitError {
		t.Fatalf("expected doctor failure, rc=%d output=%s", rc, stdout)
	}
	if !strings.Contains(stdout, "config_status=invalid_config") {
		t.Fatalf("expected invalid_config in output: %s", stdout)
	}
}
