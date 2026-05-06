package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSettings(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to parse settings: %v", err)
	}
	return settings
}

func countMatchers(t *testing.T, settings map[string]interface{}, event string) int {
	t.Helper()
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		return 0
	}
	matchers, _ := hooks[event].([]interface{})
	return len(matchers)
}

func TestSetupClaudeFreshInstall(t *testing.T) {
	home := withTempHome(t)
	rc, stdout, _ := runCLI(t, "setup", "claude")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "installed 10 hooks") {
		t.Fatalf("unexpected output: %s", stdout)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	settings := readSettings(t, settingsPath)
	hooks, _ := settings["hooks"].(map[string]interface{})
	if len(hooks) != len(claudeHookEvents) {
		t.Fatalf("expected %d hook events, got %d", len(claudeHookEvents), len(hooks))
	}
	for _, event := range claudeHookEvents {
		if countMatchers(t, settings, event) != 1 {
			t.Fatalf("expected 1 matcher for %s, got %d", event, countMatchers(t, settings, event))
		}
	}
}

func TestSetupClaudeIdempotent(t *testing.T) {
	home := withTempHome(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	runCLI(t, "setup", "claude")
	runCLI(t, "setup", "claude")
	runCLI(t, "setup", "claude")

	settings := readSettings(t, settingsPath)
	for _, event := range claudeHookEvents {
		count := countMatchers(t, settings, event)
		if count != 1 {
			t.Fatalf("expected 1 matcher for %s after 3 runs, got %d", event, count)
		}
	}
}

func TestSetupClaudeReplacesStaleConfig(t *testing.T) {
	home := withTempHome(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// Write settings with an old-style ai-attn hook (different command path).
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	oldSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "bash ~/.local/share/ai-attn/hooks/claude.sh",
							"timeout": 5,
						},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(oldSettings, "", "  ")
	os.WriteFile(settingsPath, data, 0o644)

	rc, _, _ := runCLI(t, "setup", "claude")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	settings := readSettings(t, settingsPath)

	// Stop should have exactly 1 matcher (replaced, not appended).
	if countMatchers(t, settings, "Stop") != 1 {
		t.Fatalf("expected 1 matcher for Stop after replacement, got %d", countMatchers(t, settings, "Stop"))
	}

	// The replaced hook should have the current config (timeout=10, async=true).
	hooks := settings["hooks"].(map[string]interface{})
	stopMatchers := hooks["Stop"].([]interface{})
	stopHooks := stopMatchers[0].(map[string]interface{})["hooks"].([]interface{})
	hookObj := stopHooks[0].(map[string]interface{})
	if timeout, _ := hookObj["timeout"].(float64); timeout != 10 {
		t.Fatalf("expected timeout=10 in replaced hook, got %v", hookObj["timeout"])
	}
	if async, _ := hookObj["async"].(bool); !async {
		t.Fatal("expected async=true in replaced hook")
	}

	// All 10 events should be present.
	allHooks := settings["hooks"].(map[string]interface{})
	if len(allHooks) != len(claudeHookEvents) {
		t.Fatalf("expected %d events, got %d", len(claudeHookEvents), len(allHooks))
	}
}

func TestSetupClaudePreservesNonAiAttnHooks(t *testing.T) {
	home := withTempHome(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// Write settings with a non-ai-attn hook on PostToolUse.
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	existing := map[string]interface{}{
		"model": "opus",
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Write|Edit",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "prettier --write $FILE",
						},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(settingsPath, data, 0o644)

	rc, _, _ := runCLI(t, "setup", "claude")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	settings := readSettings(t, settingsPath)

	// Model should be preserved.
	if settings["model"] != "opus" {
		t.Fatalf("expected model=opus preserved, got %v", settings["model"])
	}

	// PostToolUse should have 2 matchers: prettier + ai-attn.
	if countMatchers(t, settings, "PostToolUse") != 2 {
		t.Fatalf("expected 2 matchers for PostToolUse, got %d", countMatchers(t, settings, "PostToolUse"))
	}

	// Verify the prettier hook is still there.
	hooks := settings["hooks"].(map[string]interface{})
	matchers := hooks["PostToolUse"].([]interface{})
	found := false
	for _, m := range matchers {
		matcher := m.(map[string]interface{})
		hookList := matcher["hooks"].([]interface{})
		for _, h := range hookList {
			hook := h.(map[string]interface{})
			if cmd, _ := hook["command"].(string); cmd == "prettier --write $FILE" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected prettier hook to be preserved")
	}
}

func TestSetupAllAgents(t *testing.T) {
	home := withTempHome(t)
	rc, stdout, _ := runCLI(t, "setup")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "claude") {
		t.Fatalf("expected claude in output, got: %s", stdout)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	settings := readSettings(t, settingsPath)
	hooks, _ := settings["hooks"].(map[string]interface{})
	if len(hooks) != len(claudeHookEvents) {
		t.Fatalf("expected %d hook events, got %d", len(claudeHookEvents), len(hooks))
	}
}

func TestSetupUnknownAgent(t *testing.T) {
	withTempHome(t)
	rc, _, _ := runCLI(t, "setup", "unknown-agent")
	if rc != exitUsage {
		t.Fatalf("expected exit %d for unknown agent, got %d", exitUsage, rc)
	}
}
