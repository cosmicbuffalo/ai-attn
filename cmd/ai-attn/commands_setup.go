package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	"StopFailure",
}

var allAgents = []string{"claude"}

func cmdSetup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage: ai-attn setup [agent]

Installs ai-attn hooks into agent configuration files.
Safe to re-run — removes existing ai-attn hooks and re-adds them fresh,
preserving all other settings and non-ai-attn hooks.

If no agent is specified, sets up all supported agents.

Supported agents:
  claude    Install hooks into ~/.claude/settings.json`)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	agents := allAgents
	if fs.NArg() > 0 {
		agents = []string{fs.Arg(0)}
	}

	exitCode := exitOK
	for _, agent := range agents {
		switch agent {
		case "claude":
			if rc := setupClaude(stdout, stderr); rc != exitOK {
				exitCode = rc
			}
		default:
			fmt.Fprintf(stderr, "unsupported agent: %s\n", agent)
			fs.Usage()
			return exitUsage
		}
	}
	return exitCode
}

const aiAttnHookMarker = "ai-attn/hooks/"

func setupClaude(stdout, stderr io.Writer) int {
	settingsPath := filepath.Join(homeDir(), ".claude", "settings.json")
	hookCmd := "bash ~/.local/share/ai-attn/hooks/claude.sh"

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "failed to create directory: %v\n", err)
		return exitError
	}

	var settings map[string]interface{}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "failed to read %s: %v\n", settingsPath, err)
			return exitError
		}
		settings = map[string]interface{}{}
	} else {
		if err := json.Unmarshal(data, &settings); err != nil {
			fmt.Fprintf(stderr, "failed to parse %s: %v\n", settingsPath, err)
			return exitError
		}
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	// Remove existing ai-attn hook entries from all events before re-adding.
	for event, val := range hooks {
		hooks[event] = removeAiAttnEntries(val)
	}

	hookEntry := map[string]interface{}{
		"type":    "command",
		"command": hookCmd,
		"timeout": 10,
		"async":   true,
	}

	for _, event := range claudeHookEvents {
		matcher := map[string]interface{}{
			"matcher": "",
			"hooks":   []interface{}{hookEntry},
		}
		existing, _ := hooks[event].([]interface{})
		hooks[event] = append(existing, matcher)
	}

	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "failed to marshal settings: %v\n", err)
		return exitError
	}
	out = append(out, '\n')

	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		fmt.Fprintf(stderr, "failed to write %s: %v\n", settingsPath, err)
		return exitError
	}

	fmt.Fprintf(stdout, "claude: installed %d hooks in %s\n",
		len(claudeHookEvents), settingsPath)
	return exitOK
}

// removeAiAttnEntries removes any matcher entries that contain an ai-attn
// hook command, preserving all other entries.
func removeAiAttnEntries(eventEntry interface{}) []interface{} {
	matchers, ok := eventEntry.([]interface{})
	if !ok {
		return nil
	}
	var kept []interface{}
	for _, m := range matchers {
		if matcherHasAiAttn(m) {
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

func matcherHasAiAttn(m interface{}) bool {
	matcher, ok := m.(map[string]interface{})
	if !ok {
		return false
	}
	hookList, ok := matcher["hooks"].([]interface{})
	if !ok {
		return false
	}
	for _, h := range hookList {
		hook, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if cmd, ok := hook["command"].(string); ok && strings.Contains(cmd, aiAttnHookMarker) {
			return true
		}
	}
	return false
}
