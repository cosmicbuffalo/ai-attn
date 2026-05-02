package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// cmdHook processes a hook event from stdin, dispatching to the agent-specific handler.
func cmdHook(args []string, stdin io.Reader, _, stderr io.Writer) int {
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "", "Agent type: claude or codex")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if *agent == "" {
		fmt.Fprintln(stderr, "hook: --agent is required")
		return exitUsage
	}

	stdinBytes, err := io.ReadAll(io.LimitReader(stdin, 1<<20)) // 1 MB limit
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	input := strings.TrimSpace(string(stdinBytes))
	if input == "" {
		return exitOK
	}

	switch *agent {
	case "claude":
		return hookClaude(input, stderr)
	case "codex":
		return hookCodex(input, stderr)
	case "opencode":
		return hookOpencode(input, stderr)
	default:
		fmt.Fprintf(stderr, "hook: unknown agent %q\n", *agent)
		return exitUsage
	}
}

// hookClaude parses a Claude hook JSON payload and writes the corresponding state record.
func hookClaude(hookPayload string, stderr io.Writer) int {
	var payload map[string]any
	if err := json.Unmarshal([]byte(hookPayload), &payload); err != nil {
		return exitOK
	}

	event := stringFromMap(payload, "hook_event_name")
	notificationType := stringFromMap(payload, "notification_type")
	sessionID := stringFromMap(payload, "session_id")
	if sessionID == "" {
		sessionID = stringFromMap(payload, "conversation_id")
	}
	cwd := stringFromMap(payload, "cwd")
	if cwd == "" {
		if roots, ok := payload["workspace_roots"].([]any); ok && len(roots) > 0 {
			if s, ok := roots[0].(string); ok {
				cwd = s
			}
		}
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	var state string
	var reason string
	switch event {
	case "Notification":
		switch notificationType {
		case "permission_prompt", "elicitation_dialog":
			state = "waiting"
			reason = notificationType
		default:
			return exitOK
		}
	case "PermissionRequest":
		state = "waiting"
		reason = "permission_request"
	case "Elicitation":
		state = "waiting"
		reason = "elicitation"
	case "Stop":
		state = "done"
		reason = "agent-turn-complete"
	case "PreToolUse", "PostToolUse":
		state = "working"
		reason = event
	case "UserPromptSubmit", "ElicitationResult":
		state = "working"
		reason = event
	case "PostToolUseFailure", "StopFailure":
		state = "stopped"
		reason = event
	case "SessionEnd":
		paneID := os.Getenv("TMUX_PANE")
		key := sessionKey("claude", sessionID, paneID)
		if err := os.Remove(stateFile(key)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "ai-attn: warning: failed to remove state file: %v\n", err)
		}
		return exitOK
	default:
		return exitOK
	}

	return writeHookState(state, "claude", sessionID, cwd, reason, stderr)
}

// hookCodex parses a Codex hook payload (plain text or JSON) and writes the corresponding state record.
func hookCodex(hookPayload string, stderr io.Writer) int {
	eventName := hookPayload
	sessionID := ""
	cwd := ""

	var payload map[string]any
	if err := json.Unmarshal([]byte(hookPayload), &payload); err == nil {
		e := stringFromMap(payload, "hook_event_name")
		if e == "" {
			e = stringFromMap(payload, "type")
		}
		if e == "" {
			e = stringFromMap(payload, "event")
		}
		if e != "" {
			eventName = e
		}
		sessionID = stringFromMap(payload, "thread-id")
		if sessionID == "" {
			sessionID = stringFromMap(payload, "thread_id")
		}
		if sessionID == "" {
			sessionID = stringFromMap(payload, "session_id")
		}
		cwd = stringFromMap(payload, "cwd")
	}

	if sessionID == "" {
		sessionID = os.Getenv("CODEX_SESSION_ID")
	}
	if sessionID == "" {
		if pane := os.Getenv("TMUX_PANE"); pane != "" {
			sessionID = "codex-pane-" + strings.TrimPrefix(pane, "%")
		}
	}
	if sessionID == "" {
		sessionID = fmt.Sprintf("codex-ppid-%d", os.Getppid())
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	state, ok := matchCodexEvent(eventName)
	if !ok {
		return exitOK
	}

	return writeHookState(state, "codex", sessionID, cwd, eventName, stderr)
}

// hookOpencode parses an OpenCode hook JSON payload and writes the corresponding state record.
func hookOpencode(hookPayload string, stderr io.Writer) int {
	var payload map[string]any
	if err := json.Unmarshal([]byte(hookPayload), &payload); err != nil {
		return exitOK
	}

	eventType := stringFromMap(payload, "type")
	if eventType == "" {
		return exitOK
	}

	sessionID := ""
	if props, ok := payload["properties"].(map[string]any); ok {
		sessionID = stringFromMap(props, "sessionID")
	}
	if sessionID == "" {
		sessionID = stringFromMap(payload, "sessionID")
	}

	cwd, _ := os.Getwd()

	state, reason, ok := matchOpencodeEvent(eventType, payload)
	if !ok {
		return exitOK
	}

	if sessionID == "" {
		if pane := os.Getenv("TMUX_PANE"); pane != "" {
			sessionID = "opencode-pane-" + strings.TrimPrefix(pane, "%")
		}
	}
	if sessionID == "" {
		sessionID = fmt.Sprintf("opencode-ppid-%d", os.Getppid())
	}

	return writeHookState(state, "opencode", sessionID, cwd, reason, stderr)
}

// matchOpencodeEvent maps an OpenCode event type to an ai-attn state and reason.
func matchOpencodeEvent(eventType string, raw map[string]any) (state, reason string, ok bool) {
	switch eventType {
	case "session.status":
		if props, ok := raw["properties"].(map[string]any); ok {
			if status, ok := props["status"].(map[string]any); ok {
				statusType := stringFromMap(status, "type")
				switch statusType {
				case "busy":
					return "working", "session.status.busy", true
				case "idle":
					return "done", "session.status.idle", true
				case "retry":
					return "working", "session.status.retry", true
				}
			}
		}
		return "", "", false

	case "session.idle":
		return "done", "session.idle", true

	case "session.error":
		return "stopped", "session.error", true

	case "permission.updated":
		return "waiting", "permission.updated", true
	case "permission.replied":
		return "working", "permission.replied", true

	case "message.part.updated":
		return "working", "message.part.updated", true
	case "file.edited":
		return "working", "file.edited", true

	case "session.created":
		return "working", "session.created", true
	case "session.deleted":
		return "done", "session.deleted", true
	}

	return "", "", false
}

// matchCodexEvent maps a Codex event string to an ai-attn state using substring matching.
func matchCodexEvent(event string) (state string, ok bool) {
	lower := strings.ToLower(event)
	for _, pattern := range []string{"turn-start", "start-turn", "user_prompt", "user-prompt", "userpromptsubmit", "user_message", "user-message", "submit", "resume", "continue"} {
		if strings.Contains(lower, pattern) {
			return "working", true
		}
	}
	for _, pattern := range []string{"permission_required", "permission_request", "permissionrequest", "approval_required", "elicitation"} {
		if strings.Contains(lower, pattern) {
			return "waiting", true
		}
	}
	for _, pattern := range []string{"agent-turn-complete", "end_turn", "end-turn", "stop"} {
		if strings.Contains(lower, pattern) {
			return "done", true
		}
	}
	return "", false
}

// writeHookState is the shared tail of all hook handlers; it loads config and persists the state record.
func writeHookState(state string, agent, sessionID, cwd, reason string, stderr io.Writer) int {
	cfg := loadConfig(stderr)
	if state == "waiting" && !cfg.Enabled {
		return exitOK
	}
	if err := ensureStateDir(); err != nil {
		fmt.Fprintf(stderr, "ai-attn: failed to create state dir: %v\n", err)
		return exitError
	}

	identity := sessionIdentity{
		Agent:     agent,
		CWD:       cwd,
		SessionID: sessionID,
		PaneID:    os.Getenv("TMUX_PANE"),
	}

	if _, err := writeStateRecord(identity, state, reason); err != nil {
		fmt.Fprintf(stderr, "ai-attn: failed to write state record: %v\n", err)
		return exitError
	}

	return exitOK
}

// stringFromMap extracts a trimmed string value from a map, returning "" if absent or non-string.
func stringFromMap(dict map[string]any, key string) string {
	raw, ok := dict[key]
	if !ok || raw == nil {
		return ""
	}
	str, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(str)
}
