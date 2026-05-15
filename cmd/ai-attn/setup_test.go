package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to parse settings: %v", err)
	}
	return settings
}

func countMatchers(t *testing.T, settings map[string]any, event string) int {
	t.Helper()
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return 0
	}
	matchers, _ := hooks[event].([]any)
	return len(matchers)
}

func readTOMLConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	return config
}

// --- Claude tests ---

func TestSetupClaudeFreshInstall(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	rc, stdout, _ := runCLI(t, "setup", "claude")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "installed 11 hooks") {
		t.Fatalf("unexpected output: %s", stdout)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	settings := readSettings(t, settingsPath)
	hooks, _ := settings["hooks"].(map[string]any)
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

	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	oldSettings := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{
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

	if countMatchers(t, settings, "Stop") != 1 {
		t.Fatalf("expected 1 matcher for Stop after replacement, got %d", countMatchers(t, settings, "Stop"))
	}

	hooks := settings["hooks"].(map[string]any)
	stopMatchers := hooks["Stop"].([]any)
	stopHooks := stopMatchers[0].(map[string]any)["hooks"].([]any)
	hookObj := stopHooks[0].(map[string]any)
	if timeout, _ := hookObj["timeout"].(float64); timeout != 10 {
		t.Fatalf("expected timeout=10 in replaced hook, got %v", hookObj["timeout"])
	}
	if async, _ := hookObj["async"].(bool); !async {
		t.Fatal("expected async=true in replaced hook")
	}

	allHooks := settings["hooks"].(map[string]any)
	if len(allHooks) != len(claudeHookEvents) {
		t.Fatalf("expected %d events, got %d", len(claudeHookEvents), len(allHooks))
	}
}

func TestSetupClaudePreservesNonAiAttnHooks(t *testing.T) {
	home := withTempHome(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	existing := map[string]any{
		"model": "opus",
		"hooks": map[string]any{
			"PostToolUse": []any{
				map[string]any{
					"matcher": "Write|Edit",
					"hooks": []any{
						map[string]any{
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

	if settings["model"] != "opus" {
		t.Fatalf("expected model=opus preserved, got %v", settings["model"])
	}

	if countMatchers(t, settings, "PostToolUse") != 2 {
		t.Fatalf("expected 2 matchers for PostToolUse, got %d", countMatchers(t, settings, "PostToolUse"))
	}

	hooks := settings["hooks"].(map[string]any)
	matchers := hooks["PostToolUse"].([]any)
	found := false
	for _, m := range matchers {
		matcher := m.(map[string]any)
		hookList := matcher["hooks"].([]any)
		for _, h := range hookList {
			hook := h.(map[string]any)
			if cmd, _ := hook["command"].(string); cmd == "prettier --write $FILE" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected prettier hook to be preserved")
	}
}

// --- Codex tests ---

func TestSetupCodexFreshInstall(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	rc, stdout, _ := runCLI(t, "setup", "codex")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "codex: installed hook") {
		t.Fatalf("unexpected output: %s", stdout)
	}

	configPath := filepath.Join(home, ".codex", "config.toml")
	config := readTOMLConfig(t, configPath)

	notify, ok := config["notify"].([]any)
	if !ok {
		t.Fatalf("expected notify array, got %T", config["notify"])
	}
	if len(notify) != 2 {
		t.Fatalf("expected 2 elements in notify, got %d", len(notify))
	}
	if notify[0] != "bash" {
		t.Fatalf("expected notify[0]=bash, got %v", notify[0])
	}
	hookPath := filepath.Join(home, ".local", "share", "ai-attn", "hooks", "codex.sh")
	if notify[1] != hookPath {
		t.Fatalf("expected notify[1]=%s, got %v", hookPath, notify[1])
	}
}

func TestSetupCodexIdempotent(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)

	runCLI(t, "setup", "codex")
	runCLI(t, "setup", "codex")
	runCLI(t, "setup", "codex")

	configPath := filepath.Join(home, ".codex", "config.toml")
	config := readTOMLConfig(t, configPath)

	notify, ok := config["notify"].([]any)
	if !ok {
		t.Fatalf("expected notify array, got %T", config["notify"])
	}
	if len(notify) != 2 {
		t.Fatalf("expected 2 elements in notify after 3 runs, got %d", len(notify))
	}
}

func TestSetupCodexPreservesOtherKeys(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	os.WriteFile(configPath, []byte("model = \"gpt-4\"\napproval_policy = \"always\"\n"), 0o644)

	rc, _, _ := runCLI(t, "setup", "codex")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	config := readTOMLConfig(t, configPath)
	if config["model"] != "gpt-4" {
		t.Fatalf("expected model=gpt-4, got %v", config["model"])
	}
	if config["approval_policy"] != "always" {
		t.Fatalf("expected approval_policy=always, got %v", config["approval_policy"])
	}
	if _, ok := config["notify"]; !ok {
		t.Fatal("expected notify key to be added")
	}
}

func TestSetupCodexReplacesStaleAiAttnNotify(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	stale := "notify = [\"bash\", \"/old/path/.local/share/ai-attn/hooks/codex.sh\"]\n"
	os.WriteFile(configPath, []byte(stale), 0o644)

	rc, _, _ := runCLI(t, "setup", "codex")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	config := readTOMLConfig(t, configPath)
	notify, _ := config["notify"].([]any)
	hookPath := filepath.Join(home, ".local", "share", "ai-attn", "hooks", "codex.sh")
	if len(notify) != 2 || notify[1] != hookPath {
		t.Fatalf("expected stale ai-attn notify to be replaced with current path, got %v", notify)
	}
}

func TestSetupCodexRefusesForeignNotify(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	original := "notify = [\"bash\", \"/usr/local/bin/my-own-hook.sh\"]\n"
	os.WriteFile(configPath, []byte(original), 0o644)

	rc, _, stderr := runCLI(t, "setup", "codex")
	if rc == exitOK {
		t.Fatalf("expected non-zero exit when refusing foreign notify, got %d", rc)
	}
	if !strings.Contains(stderr, "refusing to overwrite") {
		t.Fatalf("expected refusal warning in stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, "--force") {
		t.Fatalf("expected stderr to mention --force escape hatch, got: %s", stderr)
	}
	if !strings.Contains(stderr, "/usr/local/bin/my-own-hook.sh") {
		t.Fatalf("expected stderr to surface the existing notify value, got: %s", stderr)
	}

	data, _ := os.ReadFile(configPath)
	if string(data) != original {
		t.Fatalf("expected config to be unchanged after refusal, got: %s", string(data))
	}
}

func TestSetupCodexForceOverwritesForeignNotify(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	os.WriteFile(configPath, []byte("notify = [\"bash\", \"/usr/local/bin/my-own-hook.sh\"]\n"), 0o644)

	rc, _, _ := runCLI(t, "setup", "--force", "codex")
	if rc != exitOK {
		t.Fatalf("expected exit 0 with --force, got %d", rc)
	}

	config := readTOMLConfig(t, configPath)
	notify, _ := config["notify"].([]any)
	hookPath := filepath.Join(home, ".local", "share", "ai-attn", "hooks", "codex.sh")
	if len(notify) != 2 || notify[1] != hookPath {
		t.Fatalf("expected notify to be overwritten with --force, got %v", notify)
	}
}

func TestSetupCodexRefusalDoesNotBlockOtherAgents(t *testing.T) {
	home := withTempHome(t)
	codexPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(codexPath), 0o755)
	os.WriteFile(codexPath, []byte("notify = [\"bash\", \"/usr/local/bin/my-own-hook.sh\"]\n"), 0o644)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)

	rc, stdout, _ := runCLI(t, "setup")
	if rc == exitOK {
		t.Fatalf("expected non-zero exit when one agent refuses, got %d", rc)
	}
	if !strings.Contains(stdout, "claude: installed") {
		t.Fatalf("expected claude to still be set up despite codex refusal, stdout: %s", stdout)
	}
}

// --- OpenCode tests ---

func TestSetupOpencodeFreshInstall(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755)
	rc, stdout, _ := runCLI(t, "setup", "opencode")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "opencode: installed plugin") {
		t.Fatalf("unexpected output: %s", stdout)
	}

	configPath := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	settings := readSettings(t, configPath)

	plugins, ok := settings["plugin"].([]any)
	if !ok {
		t.Fatalf("expected plugin array, got %T", settings["plugin"])
	}
	pluginPath := filepath.Join(home, ".local", "share", "ai-attn", "plugins", "opencode")
	if len(plugins) != 1 || plugins[0] != pluginPath {
		t.Fatalf("expected [%s], got %v", pluginPath, plugins)
	}
}

func TestSetupOpencodeIdempotent(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755)

	runCLI(t, "setup", "opencode")
	runCLI(t, "setup", "opencode")

	configPath := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	settings := readSettings(t, configPath)

	plugins, _ := settings["plugin"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin after 2 runs, got %d", len(plugins))
	}
}

func TestSetupOpencodePreservesExistingPlugins(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	os.WriteFile(configPath, []byte(`{"plugin": ["/some/other/plugin"]}`), 0o644)

	rc, _, _ := runCLI(t, "setup", "opencode")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	settings := readSettings(t, configPath)
	plugins, _ := settings["plugin"].([]any)
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d: %v", len(plugins), plugins)
	}

	found := false
	for _, p := range plugins {
		if p == "/some/other/plugin" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected /some/other/plugin to be preserved")
	}
}

func TestSetupOpencodeRefusesJSONCComments(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	original := `{
  // This is a comment
  "theme": "dark",
  /* block comment */
  "plugin": ["/existing/plugin"]
}
`
	os.WriteFile(configPath, []byte(original), 0o644)

	rc, _, stderr := runCLI(t, "setup", "opencode")
	if rc == exitOK {
		t.Fatalf("expected non-zero exit when refusing to strip comments, got %d", rc)
	}
	if !strings.Contains(stderr, "refusing to overwrite") {
		t.Fatalf("expected refusal warning in stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, "--force") {
		t.Fatalf("expected stderr to mention --force escape hatch, got: %s", stderr)
	}

	data, _ := os.ReadFile(configPath)
	if string(data) != original {
		t.Fatalf("expected file unchanged after refusal, got:\n%s", string(data))
	}
}

func TestSetupOpencodeForceOverwritesComments(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	jsonc := `{
  // This is a comment
  "theme": "dark",
  "plugin": ["/existing/plugin"]
}
`
	os.WriteFile(configPath, []byte(jsonc), 0o644)

	rc, _, _ := runCLI(t, "setup", "--force", "opencode")
	if rc != exitOK {
		t.Fatalf("expected exit 0 with --force, got %d", rc)
	}

	settings := readSettings(t, configPath)
	if settings["theme"] != "dark" {
		t.Fatalf("expected theme=dark preserved with --force, got %v", settings["theme"])
	}
	plugins, _ := settings["plugin"].([]any)
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(plugins))
	}
}

func TestSetupOpencodeIgnoresCommentsInsideStrings(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	os.WriteFile(configPath, []byte(`{"note": "see https://example.com/path"}`), 0o644)

	rc, _, stderr := runCLI(t, "setup", "opencode")
	if rc != exitOK {
		t.Fatalf("expected exit 0 when comment markers appear only inside strings, got %d; stderr: %s", rc, stderr)
	}
}

func TestSetupOpencodeTrailingCommas(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	os.WriteFile(configPath, []byte(`{"plugin": ["/existing/plugin",],}`), 0o644)

	rc, _, stderr := runCLI(t, "setup", "opencode")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", rc, stderr)
	}

	settings := readSettings(t, configPath)
	plugins, _ := settings["plugin"].([]any)
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d: %v", len(plugins), plugins)
	}
}

// --- Auto-detect tests ---

func TestSetupAutoDetectNoAgents(t *testing.T) {
	withTempHome(t)
	rc, stdout, _ := runCLI(t, "setup")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "No supported agents detected") {
		t.Fatalf("expected no-agents message, got: %s", stdout)
	}
}

func TestSetupAutoDetectMultiple(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)

	rc, stdout, _ := runCLI(t, "setup")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "claude:") {
		t.Fatalf("expected claude in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "codex:") {
		t.Fatalf("expected codex in output, got: %s", stdout)
	}

	// Verify configs were written
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("expected claude settings to be written: %v", err)
	}
	codexPath := filepath.Join(home, ".codex", "config.toml")
	if _, err := os.Stat(codexPath); err != nil {
		t.Fatalf("expected codex config to be written: %v", err)
	}
}

func TestSetupAllAgents(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755)

	rc, stdout, _ := runCLI(t, "setup")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "claude:") {
		t.Fatalf("expected claude in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "codex:") {
		t.Fatalf("expected codex in output, got: %s", stdout)
	}
	if !strings.Contains(stdout, "opencode:") {
		t.Fatalf("expected opencode in output, got: %s", stdout)
	}

	settings := readSettings(t, filepath.Join(home, ".claude", "settings.json"))
	hooks, _ := settings["hooks"].(map[string]any)
	if len(hooks) != len(claudeHookEvents) {
		t.Fatalf("expected %d hook events, got %d", len(claudeHookEvents), len(hooks))
	}
}

// --- Dry-run tests ---

func TestSetupDryRunClaude(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	rc, stdout, _ := runCLI(t, "setup", "--dry-run", "claude")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "(dry-run)") {
		t.Fatalf("expected dry-run in output, got: %s", stdout)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("expected settings.json to not exist in dry-run, err=%v", err)
	}
}

func TestSetupDryRunCodex(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	rc, stdout, _ := runCLI(t, "setup", "--dry-run", "codex")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "(dry-run)") {
		t.Fatalf("expected dry-run in output, got: %s", stdout)
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected config.toml to not exist in dry-run, err=%v", err)
	}
}

func TestSetupDryRunOpencode(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755)
	rc, stdout, _ := runCLI(t, "setup", "--dry-run", "opencode")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "(dry-run)") {
		t.Fatalf("expected dry-run in output, got: %s", stdout)
	}
	configPath := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected opencode.jsonc to not exist in dry-run, err=%v", err)
	}
}

func TestSetupUnknownAgent(t *testing.T) {
	withTempHome(t)
	rc, _, _ := runCLI(t, "setup", "unknown-agent")
	if rc != exitUsage {
		t.Fatalf("expected exit %d for unknown agent, got %d", exitUsage, rc)
	}
}

// --- stripJSONCComments tests ---

func TestStripJSONCComments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"no comments",
			`{"key": "value"}`,
			`{"key": "value"}`,
		},
		{
			"single-line comment",
			"{// comment\n\"key\": \"value\"}",
			"{\n\"key\": \"value\"}",
		},
		{
			"block comment",
			"{/* comment */\"key\": \"value\"}",
			"{\"key\": \"value\"}",
		},
		{
			"url in string not stripped",
			`{"url": "https://example.com"}`,
			`{"url": "https://example.com"}`,
		},
		{
			"escaped quote in string",
			`{"msg": "say \"hello\""}`,
			`{"msg": "say \"hello\""}`,
		},
		{
			"trailing comma in array",
			`{"plugin": ["a",]}`,
			`{"plugin": ["a"]}`,
		},
		{
			"trailing comma in object",
			"{\"a\": 1,\n}",
			"{\"a\": 1}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripJSONCComments(tt.input)
			if got != tt.want {
				t.Errorf("stripJSONCComments(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
