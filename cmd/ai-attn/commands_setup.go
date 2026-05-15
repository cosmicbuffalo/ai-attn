package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

var claudeHookEvents = []string{
	"Notification",
	"PermissionRequest",
	"Elicitation",
	"Stop",
	"UserPromptSubmit",
	"ElicitationResult",
	"SessionEnd",
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"StopFailure",
}

var allAgents = []string{"claude", "codex", "opencode"}

func cmdSetup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "Show what would be done without writing files")
	force := fs.Bool("force", false, "Overwrite a foreign codex notify command instead of refusing")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage: ai-attn setup [--dry-run] [--force] [agent]

Installs ai-attn hooks into agent configuration files.
Safe to re-run — removes existing ai-attn hooks and re-adds them fresh,
preserving all other settings and non-ai-attn hooks.

If no agent is specified, auto-detects installed agents and sets up
those found.

Codex supports only one global notify command. If ~/.codex/config.toml
already has a notify entry that is not an ai-attn hook, setup refuses
to overwrite it. Pass --force to overwrite anyway.

Supported agents:
  claude    Install hooks into ~/.claude/settings.json
  codex     Install notify hook into ~/.codex/config.toml
  opencode  Install plugin into ~/.config/opencode/opencode.jsonc`)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	var agents []string
	if fs.NArg() > 0 {
		name := fs.Arg(0)
		valid := false
		for _, a := range allAgents {
			if a == name {
				valid = true
				break
			}
		}
		if !valid {
			fmt.Fprintf(stderr, "unsupported agent: %s\n", name)
			fs.Usage()
			return exitUsage
		}
		agents = []string{name}
	} else {
		agents = detectAgents()
		if len(agents) == 0 {
			fmt.Fprintln(stdout, "No supported agents detected. Install an agent first, then run 'ai-attn setup'.")
			return exitOK
		}
	}

	exitCode := exitOK
	for _, agent := range agents {
		var rc int
		switch agent {
		case "claude":
			rc = setupClaude(stdout, stderr, *dryRun)
		case "codex":
			rc = setupCodex(stdout, stderr, *dryRun, *force)
		case "opencode":
			rc = setupOpencode(stdout, stderr, *dryRun, *force)
		}
		if rc != exitOK {
			exitCode = rc
		}
	}
	return exitCode
}

func detectAgents() []string {
	home := homeDir()
	checks := []struct {
		name string
		dir  string
	}{
		{"claude", filepath.Join(home, ".claude")},
		{"codex", filepath.Join(home, ".codex")},
		{"opencode", filepath.Join(home, ".config", "opencode")},
	}
	var detected []string
	for _, c := range checks {
		if info, err := os.Stat(c.dir); err == nil && info.IsDir() {
			detected = append(detected, c.name)
		}
	}
	return detected
}

const aiAttnHookMarker = "ai-attn/hooks/"
const aiAttnPluginMarker = "ai-attn/plugins/opencode"

func setupClaude(stdout, stderr io.Writer, dryRun bool) int {
	settingsPath := filepath.Join(homeDir(), ".claude", "settings.json")
	hookCmd := "bash ~/.local/share/ai-attn/hooks/claude.sh"

	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
			fmt.Fprintf(stderr, "failed to create directory: %v\n", err)
			return exitError
		}
	}

	var settings map[string]any

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "failed to read %s: %v\n", settingsPath, err)
			return exitError
		}
		settings = map[string]any{}
	} else {
		if err := json.Unmarshal(data, &settings); err != nil {
			fmt.Fprintf(stderr, "failed to parse %s: %v\n", settingsPath, err)
			return exitError
		}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	for event, val := range hooks {
		hooks[event] = removeAiAttnEntries(val)
	}

	hookEntry := map[string]any{
		"type":    "command",
		"command": hookCmd,
		"timeout": 10,
		"async":   true,
	}

	for _, event := range claudeHookEvents {
		matcher := map[string]any{
			"matcher": "",
			"hooks":   []any{hookEntry},
		}
		existing, _ := hooks[event].([]any)
		hooks[event] = append(existing, matcher)
	}

	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "failed to marshal settings: %v\n", err)
		return exitError
	}
	out = append(out, '\n')

	if dryRun {
		fmt.Fprintf(stdout, "claude: would install %d hooks in %s (dry-run)\n",
			len(claudeHookEvents), settingsPath)
		return exitOK
	}

	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		fmt.Fprintf(stderr, "failed to write %s: %v\n", settingsPath, err)
		return exitError
	}

	fmt.Fprintf(stdout, "claude: installed %d hooks in %s\n",
		len(claudeHookEvents), settingsPath)
	return exitOK
}

func setupCodex(stdout, stderr io.Writer, dryRun, force bool) int {
	configPath := filepath.Join(homeDir(), ".codex", "config.toml")
	hookPath := filepath.Join(homeDir(), ".local", "share", "ai-attn", "hooks", "codex.sh")

	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			fmt.Fprintf(stderr, "failed to create directory: %v\n", err)
			return exitError
		}
	}

	var config map[string]any

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "failed to read %s: %v\n", configPath, err)
			return exitError
		}
		config = map[string]any{}
	} else {
		if err := toml.Unmarshal(data, &config); err != nil {
			fmt.Fprintf(stderr, "failed to parse %s: %v\n", configPath, err)
			return exitError
		}
	}

	if existing, ok := config["notify"].([]any); ok && !notifyIsAiAttn(existing) && !force {
		fmt.Fprintf(stderr, "codex: refusing to overwrite existing notify in %s\n", configPath)
		fmt.Fprintf(stderr, "  existing: %s\n", formatNotify(existing))
		fmt.Fprintln(stderr, "  codex supports only one global notify command. Remove the line manually,")
		fmt.Fprintln(stderr, "  or re-run with --force to overwrite.")
		return exitError
	}

	config["notify"] = []any{"bash", hookPath}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(config); err != nil {
		fmt.Fprintf(stderr, "failed to marshal config: %v\n", err)
		return exitError
	}

	if dryRun {
		fmt.Fprintf(stdout, "codex: would install hook in %s (dry-run)\n", configPath)
		return exitOK
	}

	if err := os.WriteFile(configPath, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintf(stderr, "failed to write %s: %v\n", configPath, err)
		return exitError
	}

	fmt.Fprintf(stdout, "codex: installed hook in %s\n", configPath)
	return exitOK
}

func setupOpencode(stdout, stderr io.Writer, dryRun, force bool) int {
	configPath := filepath.Join(homeDir(), ".config", "opencode", "opencode.jsonc")
	pluginPath := filepath.Join(homeDir(), ".local", "share", "ai-attn", "plugins", "opencode")

	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			fmt.Fprintf(stderr, "failed to create directory: %v\n", err)
			return exitError
		}
	}

	var config map[string]any

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "failed to read %s: %v\n", configPath, err)
			return exitError
		}
		config = map[string]any{}
	} else {
		if hasJSONCComments(string(data)) && !force {
			fmt.Fprintf(stderr, "opencode: refusing to overwrite %s: file contains comments that would be lost\n", configPath)
			fmt.Fprintln(stderr, "  Setup re-emits the config as plain JSON, dropping // and /* */ comments.")
			fmt.Fprintln(stderr, "  Either remove the comments, wire the plugin manually (see AGENTS.md),")
			fmt.Fprintln(stderr, "  or re-run with --force to overwrite.")
			return exitError
		}
		stripped := stripJSONCComments(string(data))
		if err := json.Unmarshal([]byte(stripped), &config); err != nil {
			fmt.Fprintf(stderr, "failed to parse %s: %v\n", configPath, err)
			return exitError
		}
	}

	plugins, _ := config["plugin"].([]any)

	var kept []any
	for _, p := range plugins {
		s, ok := p.(string)
		if ok && strings.Contains(s, aiAttnPluginMarker) {
			continue
		}
		kept = append(kept, p)
	}
	kept = append(kept, pluginPath)
	config["plugin"] = kept

	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "failed to marshal config: %v\n", err)
		return exitError
	}
	out = append(out, '\n')

	if dryRun {
		fmt.Fprintf(stdout, "opencode: would install plugin in %s (dry-run)\n", configPath)
		return exitOK
	}

	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		fmt.Fprintf(stderr, "failed to write %s: %v\n", configPath, err)
		return exitError
	}

	fmt.Fprintf(stdout, "opencode: installed plugin in %s\n", configPath)
	return exitOK
}

func hasJSONCComments(s string) bool {
	inString := false
	for i := 0; i < len(s); i++ {
		if inString {
			if s[i] == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if s[i] == '"' {
				inString = false
			}
			continue
		}
		if s[i] == '"' {
			inString = true
			continue
		}
		if i+1 < len(s) && s[i] == '/' && (s[i+1] == '/' || s[i+1] == '*') {
			return true
		}
	}
	return false
}

func stripJSONCComments(s string) string {
	var out strings.Builder
	inString := false
	inBlock := false
	i := 0
	for i < len(s) {
		if inBlock {
			if i+1 < len(s) && s[i] == '*' && s[i+1] == '/' {
				inBlock = false
				i += 2
				continue
			}
			if s[i] == '\n' {
				out.WriteByte('\n')
			}
			i++
			continue
		}
		if inString {
			if s[i] == '\\' && i+1 < len(s) {
				out.WriteByte(s[i])
				out.WriteByte(s[i+1])
				i += 2
				continue
			}
			if s[i] == '"' {
				inString = false
			}
			out.WriteByte(s[i])
			i++
			continue
		}
		if s[i] == '"' {
			inString = true
			out.WriteByte(s[i])
			i++
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			inBlock = true
			i += 2
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return trailingCommaRe.ReplaceAllString(out.String(), "$1")
}

var trailingCommaRe = regexp.MustCompile(`,\s*([\]}])`)

func removeAiAttnEntries(eventEntry any) []any {
	matchers, ok := eventEntry.([]any)
	if !ok {
		return nil
	}
	var kept []any
	for _, m := range matchers {
		if matcherHasAiAttn(m) {
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

func notifyIsAiAttn(notify []any) bool {
	for _, v := range notify {
		if s, ok := v.(string); ok && strings.Contains(s, aiAttnHookMarker) {
			return true
		}
	}
	return false
}

func formatNotify(notify []any) string {
	parts := make([]string, 0, len(notify))
	for _, v := range notify {
		if s, ok := v.(string); ok {
			parts = append(parts, fmt.Sprintf("%q", s))
		} else {
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func matcherHasAiAttn(m any) bool {
	matcher, ok := m.(map[string]any)
	if !ok {
		return false
	}
	hookList, ok := matcher["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range hookList {
		hook, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hook["command"].(string); ok && strings.Contains(cmd, aiAttnHookMarker) {
			return true
		}
	}
	return false
}
