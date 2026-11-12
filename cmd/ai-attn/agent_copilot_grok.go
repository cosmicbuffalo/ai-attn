package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// --- GitHub Copilot CLI ---
//
// Copilot loads every *.json file in ~/.copilot/hooks ($COPILOT_HOME/hooks),
// so ai-attn owns one file there and never edits Copilot's own config.
// camelCase events send camelCase payloads (sessionId, cwd) with no event
// name, so each hook passes its event as an argument. The notification hook
// is fire-and-forget: a "waiting" write can land after the turn's agentStop,
// which hookCopilot drops. Accurate waiting needs Copilot CLI 1.0.26 or later;
// before that permission_prompt fired for every tool call.

const copilotHookMarker = "ai-attn/hooks/copilot.sh"

var copilotHookEvents = []struct{ event, matcher string }{
	{"userPromptSubmitted", ""},
	{"postToolUse", ""},
	{"postToolUseFailure", ""},
	{"notification", "permission_prompt|elicitation_dialog"},
	{"agentStop", ""},
	{"sessionEnd", ""},
}

func copilotHome() string {
	return envDir("COPILOT_HOME", filepath.Join(homeDir(), ".copilot"))
}

func copilotHooksFile() string {
	return filepath.Join(copilotHome(), "hooks", "ai-attn.json")
}

func hookCopilot(hookPayload, event string, stderr io.Writer) int {
	var payload map[string]any
	if err := json.Unmarshal([]byte(hookPayload), &payload); err != nil {
		return exitOK
	}
	if event == "" {
		event = stringFromMap(payload, "hook_event_name")
	}
	sessionID := stringFromMap(payload, "sessionId")
	if sessionID == "" {
		sessionID = stringFromMap(payload, "session_id")
	}
	if sessionID == "" {
		sessionID = fallbackSessionID("copilot")
	}
	cwd := cwdOrWorkdir(stringFromMap(payload, "cwd"))

	var state, reason string
	switch event {
	case "userPromptSubmitted", "UserPromptSubmit":
		state, reason = "working", "userPromptSubmitted"
	case "postToolUse", "postToolUseFailure", "PostToolUse", "PostToolUseFailure":
		state, reason = "working", event
	case "notification", "Notification":
		kind := stringFromMap(payload, "notification_type")
		if kind != "permission_prompt" && kind != "elicitation_dialog" {
			return exitOK
		}
		if copilotTurnEnded(sessionID) {
			return exitOK
		}
		state, reason = "waiting", kind
	case "agentStop", "Stop":
		state, reason = "done", "agent-turn-complete"
	case "sessionEnd", "SessionEnd":
		removeSessionRecord("copilot", sessionID, stderr)
		return exitOK
	default:
		return exitOK
	}
	return writeHookState(state, "copilot", sessionID, cwd, reason, stderr)
}

// copilotTurnEnded reports whether the session's record already marks the
// turn as over. Every permission prompt belongs to a turn that opens with a
// synchronous userPromptSubmitted, so a notification arriving after
// agentStop is a late write from the turn that just ended.
func copilotTurnEnded(sessionID string) bool {
	key := sessionKey("copilot", sessionID, os.Getenv("TMUX_PANE"), tmuxSocket())
	existing, err := readRecord(stateFile(key))
	return err == nil && existing.State == "done"
}

func setupCopilot(stdout, stderr io.Writer, dryRun bool) int {
	hooks := map[string]any{}
	for _, h := range copilotHookEvents {
		entry := map[string]any{
			"type":       "command",
			"bash":       hookScriptCommand("copilot.sh", h.event),
			"timeoutSec": 10,
		}
		if h.matcher != "" {
			entry["matcher"] = h.matcher
		}
		hooks[h.event] = []any{entry}
	}
	out, err := marshalJSONFile(map[string]any{"version": 1, "hooks": hooks})
	if err != nil {
		fmt.Fprintf(stderr, "failed to marshal hooks: %v\n", err)
		return exitError
	}
	return writeSetupFile(stdout, stderr, "copilot", copilotHooksFile(), out,
		fmt.Sprintf("%d hooks", len(copilotHookEvents)), dryRun)
}

// --- Grok Build (xai-org/grok-build) ---
//
// Grok loads every *.json file in ~/.grok/hooks ($GROK_HOME/hooks), in
// Claude's group shape with timeouts in seconds, so ai-attn owns one file
// there. Payloads are camelCase (sessionId, cwd, notificationType) and also
// carry Claude's PascalCase hook_event_name. Grok runs Claude Code's hooks
// too by default ([compat.claude]), so hookClaude hands those to hookGrok.
//
// The Notification matcher matters: Grok also sends idle_prompt about a
// minute after every turn, which must not turn "done" into "waiting".

const grokHookMarker = "ai-attn/hooks/grok.sh"

var grokHookEvents = []struct{ event, matcher string }{
	{"UserPromptSubmit", ""},
	{"PostToolUse", ""},
	{"PostToolUseFailure", ""},
	{"Notification", "permission_prompt|elicitation_dialog"},
	{"Stop", ""},
	{"StopCancelled", ""},
	{"StopFailure", ""},
	{"SessionEnd", ""},
}

func grokHome() string {
	return envDir("GROK_HOME", filepath.Join(homeDir(), ".grok"))
}

func grokHooksFile() string {
	return filepath.Join(grokHome(), "hooks", "ai-attn.json")
}

// runningInGrok reports whether the hook was started by Grok Build, which
// exports these to every hook process.
func runningInGrok() bool {
	return os.Getenv("GROK_HOOK_EVENT") != "" || os.Getenv("GROK_SESSION_ID") != ""
}

func hookGrok(hookPayload string, stderr io.Writer) int {
	var payload map[string]any
	if err := json.Unmarshal([]byte(hookPayload), &payload); err != nil {
		return exitOK
	}
	// Subagent turns report under the parent session; only the parent's
	// own turn says whether the user is needed.
	if stringFromMap(payload, "subagentType") != "" {
		return exitOK
	}
	sessionID := stringFromMap(payload, "sessionId")
	if sessionID == "" {
		sessionID = stringFromMap(payload, "session_id")
	}
	if sessionID == "" {
		sessionID = os.Getenv("GROK_SESSION_ID")
	}
	if sessionID == "" {
		sessionID = fallbackSessionID("grok")
	}
	cwd := cwdOrWorkdir(stringFromMap(payload, "cwd"))

	var state, reason string
	switch event := stringFromMap(payload, "hook_event_name"); event {
	case "UserPromptSubmit", "PostToolUse", "PostToolUseFailure":
		state, reason = "working", event
	case "Notification":
		kind := stringFromMap(payload, "notificationType")
		if kind == "" {
			kind = stringFromMap(payload, "notification_type")
		}
		if kind != "permission_prompt" && kind != "elicitation_dialog" {
			return exitOK
		}
		state, reason = "waiting", kind
	case "Stop":
		state, reason = "done", "agent-turn-complete"
	case "StopCancelled":
		// An interrupted or declined turn is back at the prompt.
		state, reason = "done", event
	case "StopFailure":
		state, reason = "stopped", event
	case "SessionEnd":
		removeSessionRecord("grok", sessionID, stderr)
		return exitOK
	default:
		return exitOK
	}
	return writeHookState(state, "grok", sessionID, cwd, reason, stderr)
}

func setupGrok(stdout, stderr io.Writer, dryRun bool) int {
	hooks := map[string]any{}
	for _, h := range grokHookEvents {
		hooks[h.event] = []any{claudeShapedHookGroup(hookScriptCommand("grok.sh"), h.matcher, 10)}
	}
	out, err := marshalJSONFile(map[string]any{"hooks": hooks})
	if err != nil {
		fmt.Fprintf(stderr, "failed to marshal hooks: %v\n", err)
		return exitError
	}
	return writeSetupFile(stdout, stderr, "grok", grokHooksFile(), out,
		fmt.Sprintf("%d hooks", len(grokHookEvents)), dryRun)
}
