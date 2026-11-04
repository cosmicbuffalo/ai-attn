package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strconv"
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

// codexHooks returns the hook groups per event in home's Codex config.toml.
func codexHooks(t *testing.T, home string) map[string][]map[string]any {
	t.Helper()
	configPath := filepath.Join(home, ".codex", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	events, err := codexHookGroups(configPath, data)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// assertCodexHooksInstalled checks config.toml has exactly one ai-attn hook
// per Codex event, and that no ai-attn hooks remain in hooks.json.
func assertCodexHooksInstalled(t *testing.T, home string) {
	t.Helper()
	events := codexHooks(t, home)
	for _, event := range codexHookEvents {
		var commands []string
		for _, group := range events[event] {
			for _, hook := range asMaps(group["hooks"]) {
				if hook["type"] != "command" {
					t.Fatalf("expected command hook for %s, got %v", event, hook["type"])
				}
				command, _ := hook["command"].(string)
				if strings.Contains(command, "ai-attn/hooks/codex.sh") {
					commands = append(commands, command)
				}
			}
		}
		if len(commands) != 1 {
			t.Fatalf("expected 1 ai-attn hook for %s, got %d: %v", event, len(commands), commands)
		}
	}
	if data, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json")); err == nil && strings.Contains(string(data), "ai-attn/hooks/codex.sh") {
		t.Fatalf("expected no ai-attn hooks left in hooks.json:\n%s", data)
	}
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
	if !strings.Contains(stdout, "codex: installed 6 hooks") {
		t.Fatalf("unexpected output: %s", stdout)
	}

	assertCodexHooksInstalled(t, home)
}

func TestSetupCodexIdempotent(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)

	runCLI(t, "setup", "codex")
	runCLI(t, "setup", "codex")
	runCLI(t, "setup", "codex")

	assertCodexHooksInstalled(t, home)
}

// Another tool's marked hooks block and the file's comments survive setup,
// and re-running setup replaces ai-attn's block in place.
func TestSetupCodexPreservesOtherHooksAndComments(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	original := `# my settings
model = "gpt-5"

# peon-ping Codex hooks begin
[[hooks.Stop]]

[[hooks.Stop.hooks]]
type = "command"
command = "peon.sh"
# peon-ping Codex hooks end
`
	os.WriteFile(configPath, []byte(original), 0o644)

	runCLI(t, "setup", "codex")
	runCLI(t, "setup", "codex")

	data, _ := os.ReadFile(configPath)
	if !strings.HasPrefix(string(data), original) {
		t.Fatalf("expected original content kept at the top, got:\n%s", data)
	}
	if n := strings.Count(string(data), codexBlockBegin); n != 1 {
		t.Fatalf("expected 1 ai-attn block, got %d:\n%s", n, data)
	}
	assertCodexHooksInstalled(t, home)
	stop := codexHooks(t, home)["Stop"]
	if len(stop) != 2 {
		t.Fatalf("expected peon-ping's and ai-attn's Stop groups, got %d", len(stop))
	}
	if cmd := asMaps(stop[0]["hooks"])[0]["command"]; cmd != "peon.sh" {
		t.Fatalf("expected peon-ping's group to stay first (so its trust key is unchanged), got %v", cmd)
	}
}

// Hooks from older setups move out of hooks.json; other tools' hooks there
// are kept, and a file left empty is removed.
func TestSetupCodexMovesHooksOutOfHooksJSON(t *testing.T) {
	for _, tc := range []struct {
		name, other string
		keepFile    bool
	}{
		{"only ai-attn", "", false},
		{"with other hooks", `{"matcher":"","hooks":[{"type":"command","command":"notify-me"}]},`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := withTempHome(t)
			hooksPath := filepath.Join(home, ".codex", "hooks.json")
			os.MkdirAll(filepath.Dir(hooksPath), 0o755)
			os.WriteFile(hooksPath, []byte(`{"hooks":{"Stop":[`+tc.other+`{"matcher":"","hooks":[{"type":"command","command":"bash '/x/.local/share/ai-attn/hooks/codex.sh'"}]}]}}`), 0o644)

			rc, stdout, stderr := runCLI(t, "setup", "codex")
			if rc != exitOK {
				t.Fatalf("rc=%d stderr=%s", rc, stderr)
			}
			if !strings.Contains(stdout, "moved ai-attn hooks out of "+hooksPath) {
				t.Fatalf("expected migration message, got: %s", stdout)
			}
			assertCodexHooksInstalled(t, home)
			data, err := os.ReadFile(hooksPath)
			if tc.keepFile {
				if err != nil || !strings.Contains(string(data), "notify-me") {
					t.Fatalf("expected other hooks kept in hooks.json, err=%v data=%s", err, data)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("expected empty hooks.json to be removed, err=%v data=%s", err, data)
			}
		})
	}
}

// Setup refuses to write a config.toml that would not parse, instead of
// leaving Codex unable to start.
func TestSetupCodexRefusesUnmergeableHooks(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	original := "hooks = { Stop = [] }\n"
	os.WriteFile(configPath, []byte(original), 0o644)

	rc, _, stderr := runCLI(t, "setup", "codex")
	if rc != exitError || !strings.Contains(stderr, "refusing to write") {
		t.Fatalf("expected refusal, rc=%d stderr=%s", rc, stderr)
	}
	if data, _ := os.ReadFile(configPath); string(data) != original {
		t.Fatalf("expected config untouched, got:\n%s", data)
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
	if _, ok := config["notify"]; ok {
		t.Fatal("did not expect setup codex to add notify")
	}
}

func TestSetupCodexRemovesStaleAiAttnNotify(t *testing.T) {
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
	if _, ok := config["notify"]; ok {
		t.Fatalf("expected stale ai-attn notify to be removed, got %v", config["notify"])
	}
	assertCodexHooksInstalled(t, home)
}

func TestSetupCodexPreservesForeignNotify(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	original := "notify = [\"bash\", \"/usr/local/bin/my-own-hook.sh\"]\n"
	os.WriteFile(configPath, []byte(original), 0o644)

	rc, _, stderr := runCLI(t, "setup", "codex")
	if rc != exitOK {
		t.Fatalf("expected setup to preserve foreign notify, rc=%d stderr=%s", rc, stderr)
	}

	data, _ := os.ReadFile(configPath)
	if !strings.HasPrefix(string(data), original) {
		t.Fatalf("expected config to be unchanged, got: %s", string(data))
	}
	assertCodexHooksInstalled(t, home)
}

func TestSetupCodexPreservesNotifyWrapper(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	wrapper := filepath.Join(home, ".local", "share", "ai-attn", "hooks", "codex-multi.sh")
	original := "notify = [\"bash\", \"" + wrapper + "\"]\n"
	os.WriteFile(configPath, []byte(original), 0o644)

	rc, _, stderr := runCLI(t, "setup", "codex")
	if rc != exitOK {
		t.Fatalf("expected setup to preserve wrapper notify, rc=%d stderr=%s", rc, stderr)
	}
	data, _ := os.ReadFile(configPath)
	if !strings.HasPrefix(string(data), original) {
		t.Fatalf("expected config unchanged, got:\n%s", string(data))
	}
	assertCodexHooksInstalled(t, home)
}

func TestSetupClaudePreservesNonCanonicalWrapper(t *testing.T) {
	home := withTempHome(t)
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	wrapper := filepath.Join(home, ".local", "share", "ai-attn", "hooks", "claude-multi.sh")
	settings := map[string]any{
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "bash " + wrapper,
							"timeout": 10,
						},
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(settingsPath, data, 0o644)

	rc, _, _ := runCLI(t, "setup", "claude")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	result := readSettings(t, settingsPath)
	hooks := result["hooks"].(map[string]any)
	stopMatchers := hooks["Stop"].([]any)
	if len(stopMatchers) != 2 {
		t.Fatalf("expected wrapper matcher kept and canonical matcher added (2 total), got %d", len(stopMatchers))
	}

	foundWrapper := false
	foundCanonical := false
	for _, m := range stopMatchers {
		matcher := m.(map[string]any)
		hookList := matcher["hooks"].([]any)
		for _, h := range hookList {
			cmd, _ := h.(map[string]any)["command"].(string)
			if strings.Contains(cmd, "claude-multi.sh") {
				foundWrapper = true
			}
			if strings.HasSuffix(cmd, "ai-attn/hooks/claude.sh") {
				foundCanonical = true
			}
		}
	}
	if !foundWrapper {
		t.Fatal("expected wrapper command to be preserved")
	}
	if !foundCanonical {
		t.Fatal("expected canonical ai-attn command to be added alongside wrapper")
	}
}

func TestSetupCodexForceStillPreservesForeignNotify(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(configPath), 0o755)
	original := "notify = [\"bash\", \"/usr/local/bin/my-own-hook.sh\"]\n"
	os.WriteFile(configPath, []byte(original), 0o644)

	rc, _, _ := runCLI(t, "setup", "--force", "codex")
	if rc != exitOK {
		t.Fatalf("expected exit 0 with --force, got %d", rc)
	}

	data, _ := os.ReadFile(configPath)
	if !strings.HasPrefix(string(data), original) {
		t.Fatalf("expected foreign notify to be preserved, got %s", string(data))
	}
	assertCodexHooksInstalled(t, home)
}

func TestSetupCodexForeignNotifyDoesNotBlockOtherAgents(t *testing.T) {
	home := withTempHome(t)
	codexPath := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(codexPath), 0o755)
	os.WriteFile(codexPath, []byte("notify = [\"bash\", \"/usr/local/bin/my-own-hook.sh\"]\n"), 0o644)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)

	rc, stdout, _ := runCLI(t, "setup")
	if rc != exitOK {
		t.Fatalf("expected setup success, got %d", rc)
	}
	if !strings.Contains(stdout, "claude: installed") {
		t.Fatalf("expected claude to be set up, stdout: %s", stdout)
	}
	if !strings.Contains(stdout, "codex: installed") {
		t.Fatalf("expected codex to be set up, stdout: %s", stdout)
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
	assertCodexHooksInstalled(t, home)
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
		{
			"comma-bracket sequence inside string is preserved",
			`{"label": "items, ]"}`,
			`{"label": "items, ]"}`,
		},
		{
			"comma-brace sequence inside string is preserved",
			`{"msg": "end, }"}`,
			`{"msg": "end, }"}`,
		},
		{
			"escaped quote does not desync string tracking",
			`{"a": "say \"hi, ]\""}`,
			`{"a": "say \"hi, ]\""}`,
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

// museBundle writes a minimal plugin bundle where setupMuse expects it.
func museBundle(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".local", "share", "ai-attn", "plugins", "muse")
	if err := os.MkdirAll(filepath.Join(dir, ".muse-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".muse-plugin", "plugin.json"), []byte(`{"schemaVersion":1}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

// fakeMuse puts a stub `muse` on PATH that appends its argv to a log file, so
// tests can assert what setup invoked without needing Muse installed.
func fakeMuse(t *testing.T, home string, exitCode int) string {
	t.Helper()
	binDir := filepath.Join(home, "fakebin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fakebin: %v", err)
	}
	logPath := filepath.Join(home, "muse-invocations.log")
	script := "#!/bin/sh\necho \"$@\" >> " + logPath + "\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "muse"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake muse: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// TestDetectAgentsFindsMuse verifies ~/.config/muse marks Muse as installed.
func TestDetectAgentsFindsMuse(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".config", "muse"), 0o755)

	found := false
	for _, a := range detectAgents() {
		if a == "muse" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected muse in detectAgents(), got %v", detectAgents())
	}
}

// TestSetupMuseInstallsPlugin verifies setup drives `muse plugins install`
// followed by `muse plugins approve` — the trust step is required or the hooks
// stay inert.
func TestSetupMuseInstallsPlugin(t *testing.T) {
	home := withTempHome(t)
	bundle := museBundle(t, home)
	logPath := fakeMuse(t, home, 0)

	rc, stdout, stderr := runCLI(t, "setup", "muse")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", rc, stderr)
	}
	if !strings.Contains(stdout, "muse: installed plugin") {
		t.Fatalf("unexpected output: %s", stdout)
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	got := string(logged)
	if !strings.Contains(got, "plugins install "+bundle+" --scope user") {
		t.Fatalf("expected install invocation, got %q", got)
	}
	if !strings.Contains(got, "plugins approve ai-attn") {
		t.Fatalf("expected approve invocation, got %q", got)
	}
}

// TestSetupMuseDryRunWritesNothing verifies --dry-run never invokes Muse.
func TestSetupMuseDryRunWritesNothing(t *testing.T) {
	home := withTempHome(t)
	museBundle(t, home)
	logPath := fakeMuse(t, home, 0)

	rc, stdout, _ := runCLI(t, "setup", "--dry-run", "muse")
	if rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}
	if !strings.Contains(stdout, "would install plugin") {
		t.Fatalf("unexpected output: %s", stdout)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run invoked muse, err=%v", err)
	}
}

// TestSetupMuseMissingBundleIsError verifies a missing bundle reports clearly
// rather than shelling out and failing obscurely.
func TestSetupMuseMissingBundleIsError(t *testing.T) {
	home := withTempHome(t)
	fakeMuse(t, home, 0)

	rc, _, stderr := runCLI(t, "setup", "muse")
	if rc == exitOK {
		t.Fatalf("expected non-zero exit for missing bundle")
	}
	if !strings.Contains(stderr, "plugin bundle not found") {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
}

// TestSetupMuseReportsGateFailure verifies a failing `muse plugins` call
// surfaces Muse's own message — the common case being a build where the
// plugin subsystem is gated off.
func TestSetupMuseReportsGateFailure(t *testing.T) {
	home := withTempHome(t)
	museBundle(t, home)
	fakeMuse(t, home, 1)

	rc, _, stderr := runCLI(t, "setup", "muse")
	if rc == exitOK {
		t.Fatalf("expected non-zero exit when muse rejects the install")
	}
	if !strings.Contains(stderr, "plugins are unavailable") && !strings.Contains(stderr, "failed") {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
}

// TestSetupClaudeInTurnHooksAreSync verifies that in-turn events install as
// synchronous hooks so their writes cannot land after Stop, while the rest stay async.
func TestSetupClaudeInTurnHooksAreSync(t *testing.T) {
	home := withTempHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	if rc, _, _ := runCLI(t, "setup", "claude"); rc != exitOK {
		t.Fatalf("expected exit 0, got %d", rc)
	}

	settings := readSettings(t, filepath.Join(home, ".claude", "settings.json"))
	hooks := settings["hooks"].(map[string]any)
	for _, event := range claudeHookEvents {
		hookObj := hooks[event].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
		_, hasAsync := hookObj["async"]
		if claudeSyncHookEvents[event] && hasAsync {
			t.Fatalf("expected %s hook to be synchronous, got async=%v", event, hookObj["async"])
		}
		if !claudeSyncHookEvents[event] && hookObj["async"] != true {
			t.Fatalf("expected %s hook to be async", event)
		}
	}
}

// TestSetupCortexPreservesOtherHooks verifies cortex setup keeps non-ai-attn
// hooks and stays idempotent across re-runs.
func TestSetupCortexPreservesOtherHooks(t *testing.T) {
	home := withTempHome(t)
	path := filepath.Join(home, ".snowflake", "cortex", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	other := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"peon.sh"}]}]}}`
	if err := os.WriteFile(path, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if rc, out, errOut := runCLI(t, "setup", "cortex"); rc != exitOK {
			t.Fatalf("setup rc=%d out=%s err=%s", rc, out, errOut)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, event := range claudeHookEvents {
		n := 0
		for _, m := range cfg.Hooks[event] {
			for _, h := range m.Hooks {
				if strings.Contains(h.Command, cortexHookSuffix) {
					n++
				}
			}
		}
		if n != 1 {
			t.Fatalf("event %s: want 1 ai-attn hook, got %d", event, n)
		}
	}
	if !strings.Contains(string(data), "peon.sh") {
		t.Fatal("setup dropped a non-ai-attn hook")
	}
}

func TestSetupCortexReplacesLegacyClaudeHooks(t *testing.T) {
	home := withTempHome(t)
	path := filepath.Join(home, ".snowflake", "cortex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"bash ~/.local/share/ai-attn/hooks/claude.sh"},{"type":"command","command":"peon.sh"}]}]}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, _, errOut := runCLI(t, "setup", "cortex"); rc != exitOK {
		t.Fatal(errOut)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), claudeHookSuffix) || !strings.Contains(string(data), "peon.sh") {
		t.Fatalf("legacy hooks were not replaced while preserving other hooks: %s", data)
	}
}

func TestSetupCortexHandlesNullConfig(t *testing.T) {
	home := withTempHome(t)
	path := filepath.Join(home, ".snowflake", "cortex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, _, errOut := runCLI(t, "setup", "cortex"); rc != exitOK {
		t.Fatal(errOut)
	}
}

func TestSetupHonoursFlagsAfterTheAgent(t *testing.T) {
	for _, args := range [][]string{
		{"setup", "cortex", "--dry-run"},
		{"setup", "--dry-run", "cortex"},
		{"setup", "claude", "-dry-run"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			home := withTempHome(t)
			os.MkdirAll(filepath.Join(home, ".snowflake", "cortex"), 0o755)
			os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
			rc, out, errOut := runCLI(t, args...)
			if rc != exitOK {
				t.Fatalf("rc=%d out=%s err=%s", rc, out, errOut)
			}
			if !strings.Contains(out, "(dry-run)") {
				t.Fatalf("output %q does not report a dry run", out)
			}
			for _, path := range []string{
				filepath.Join(home, ".snowflake", "cortex", "hooks.json"),
				filepath.Join(home, ".claude", "settings.json"),
			} {
				if _, err := os.Stat(path); err == nil {
					t.Fatalf("dry run wrote %s", path)
				}
			}
		})
	}
}

func TestSetupRejectsMoreThanOneAgent(t *testing.T) {
	withTempHome(t)
	rc, _, errOut := runCLI(t, "setup", "claude", "codex")
	if rc != exitUsage {
		t.Fatalf("rc=%d, want usage error", rc)
	}
	if !strings.Contains(errOut, "at most one agent") {
		t.Fatalf("stderr %q does not explain the error", errOut)
	}
}

func TestParseInterspersedStopsAtDoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "")
	got, err := parseInterspersed(fs, []string{"a", "--dry-run", "--", "--b"})
	if err != nil {
		t.Fatal(err)
	}
	if !*dry || strings.Join(got, ",") != "a,--b" {
		t.Fatalf("dry=%v positional=%q", *dry, got)
	}
}
