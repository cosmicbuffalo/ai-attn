package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// --- Gemini CLI ---
//
// Hooks live in the "hooks" object of ~/.gemini/settings.json, in Claude's
// group shape, with timeouts in milliseconds. Every hook runs synchronously
// and inherits the agent's environment (TMUX_PANE included), so there is no
// late-write race to guard against. Payloads arrive on stdin with
// hook_event_name, session_id and cwd.

const geminiHookMarker = "ai-attn/hooks/gemini.sh"

// geminiHookEvents maps each Gemini event ai-attn hooks to its matcher.
// Notification's only type is ToolPermission, which also covers ask_user
// and plan confirmations.
var geminiHookEvents = []struct{ event, matcher string }{
	{"BeforeAgent", ""},
	{"AfterTool", ""},
	{"Notification", "ToolPermission"},
	{"AfterAgent", ""},
	{"SessionEnd", ""},
}

// geminiHome is Gemini CLI's config directory. GEMINI_CLI_HOME replaces the
// home directory it lives in.
func geminiHome() string {
	return filepath.Join(envDir("GEMINI_CLI_HOME", homeDir()), ".gemini")
}

// geminiInstalled reports whether Gemini CLI itself is present. ~/.gemini
// alone is not enough: Antigravity keeps its own files there too.
func geminiInstalled() bool {
	return fileExists(filepath.Join(geminiHome(), "settings.json")) || onPath("gemini")
}

func hookGemini(hookPayload string, stderr io.Writer) int {
	var payload map[string]any
	if err := json.Unmarshal([]byte(hookPayload), &payload); err != nil {
		return exitOK
	}
	sessionID := stringFromMap(payload, "session_id")
	if sessionID == "" {
		sessionID = os.Getenv("GEMINI_SESSION_ID")
	}
	if sessionID == "" {
		sessionID = fallbackSessionID("gemini")
	}
	cwd := cwdOrWorkdir(stringFromMap(payload, "cwd"))

	var state, reason string
	switch event := stringFromMap(payload, "hook_event_name"); event {
	case "BeforeAgent", "AfterTool":
		state, reason = "working", event
	case "Notification":
		kind := stringFromMap(payload, "notification_type")
		if kind != "ToolPermission" {
			return exitOK
		}
		state, reason = "waiting", kind
	case "AfterAgent":
		state, reason = "done", "agent-turn-complete"
	case "SessionEnd":
		// /clear ends the session and starts a new one; either way this
		// session's record is finished.
		removeSessionRecord("gemini", sessionID, stderr)
		return exitOK
	default:
		return exitOK
	}
	return writeHookState(state, "gemini", sessionID, cwd, reason, stderr)
}

func setupGemini(stdout, stderr io.Writer, dryRun, force bool) int {
	settingsPath := filepath.Join(geminiHome(), "settings.json")
	settings, err := readJSONObject("gemini", settingsPath, true, force)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	groups := map[string][]any{}
	for _, h := range geminiHookEvents {
		groups[h.event] = []any{claudeShapedHookGroup(hookScriptCommand("gemini.sh"), h.matcher, 10000)}
	}
	replaceHookGroups(hooks, geminiHookMarker, groups)
	settings["hooks"] = hooks
	out, err := marshalJSONFile(settings)
	if err != nil {
		fmt.Fprintf(stderr, "failed to marshal settings: %v\n", err)
		return exitError
	}
	return writeSetupFile(stdout, stderr, "gemini", settingsPath, out,
		fmt.Sprintf("%d hooks", len(geminiHookEvents)), dryRun)
}

// --- Antigravity CLI (agy) ---
//
// Global hooks live in ~/.gemini/config/hooks.json as named groups, each a
// map of event name to handlers; ai-attn owns the "ai-attn" group. There is
// no permission or notification event and no session-end event, so agy
// records only working and done. Payloads are camelCase (conversationId,
// workspacePaths, and from 1.5.4 hookEventName and cwd), so the hook script
// passes the event name as an argument for older releases. A hook's stdout
// must be valid JSON for its event; the script prints {}.

const (
	agyHookGroup  = "ai-attn"
	agyHookMarker = "ai-attn/hooks/agy.sh"
)

var agyHookEvents = []string{"PreInvocation", "PostToolUse", "Stop"}

func agyHooksPath() string {
	return filepath.Join(homeDir(), ".gemini", "config", "hooks.json")
}

func agyInstalled() bool {
	return dirExists(filepath.Join(homeDir(), ".gemini", "antigravity-cli")) || onPath("agy")
}

func hookAgy(hookPayload, event string, stderr io.Writer) int {
	var payload map[string]any
	if err := json.Unmarshal([]byte(hookPayload), &payload); err != nil {
		return exitOK
	}
	if event == "" {
		event = stringFromMap(payload, "hookEventName")
	}
	sessionID := stringFromMap(payload, "conversationId")
	if sessionID == "" {
		sessionID = os.Getenv("ANTIGRAVITY_CONVERSATION_ID")
	}
	if sessionID == "" {
		sessionID = fallbackSessionID("agy")
	}
	cwd := stringFromMap(payload, "cwd")
	if cwd == "" {
		if paths, ok := payload["workspacePaths"].([]any); ok && len(paths) > 0 {
			cwd, _ = paths[0].(string)
		}
	}
	cwd = cwdOrWorkdir(cwd)

	var state, reason string
	switch event {
	case "PreInvocation", "PostToolUse":
		state, reason = "working", event
	case "Stop":
		// fullyIdle false means background tasks are still running, so
		// the agent is not yet back at its prompt.
		if idle, ok := payload["fullyIdle"].(bool); ok && !idle {
			return exitOK
		}
		state, reason = "done", "agent-turn-complete"
	default:
		return exitOK
	}
	return writeHookState(state, "agy", sessionID, cwd, reason, stderr)
}

func setupAgy(stdout, stderr io.Writer, dryRun bool) int {
	hooksPath := agyHooksPath()
	config, err := readJSONObject("agy", hooksPath, false, false)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	group := map[string]any{"enabled": true}
	for _, event := range agyHookEvents {
		group[event] = []any{map[string]any{
			"type":    "command",
			"command": hookScriptCommand("agy.sh", event),
			"timeout": 10,
		}}
	}
	config[agyHookGroup] = group
	out, err := marshalJSONFile(config)
	if err != nil {
		fmt.Fprintf(stderr, "failed to marshal hooks: %v\n", err)
		return exitError
	}
	return writeSetupFile(stdout, stderr, "agy", hooksPath, out,
		fmt.Sprintf("%d hooks", len(agyHookEvents)), dryRun)
}
