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
	agent := fs.String("agent", "", "Agent type: claude, codex, opencode, or muse")
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
	case "muse":
		return hookMuse(input, stderr)
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
		identity := sessionIdentity{
			Agent:     "claude",
			SessionID: sessionID,
			PaneID:    paneID,
			Socket:    tmuxSocket(),
		}
		key := sessionKey(identity.Agent, identity.SessionID, identity.PaneID, identity.Socket)
		if err := os.Remove(stateFile(key)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "ai-attn: warning: failed to remove state file: %v\n", err)
		}
		if err := removeCompatibleLegacyRecord(identity, key); err != nil {
			fmt.Fprintf(stderr, "ai-attn: warning: failed to remove legacy state file: %v\n", err)
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

	sessionID := opencodeSessionID(payload)

	cwd, _ := os.Getwd()

	if eventType == "session.deleted" {
		if sessionID != "" {
			removeSessionRecord("opencode", sessionID, stderr)
		}
		return exitOK
	}

	state, reason, ok := matchOpencodeEvent(eventType, payload)
	if !ok {
		return exitOK
	}
	// OpenCode also publishes session.error for failures outside any session
	// (a plugin or skill that fails to load). Keyed to the pane, such an
	// error would land on a record no later event ever clears.
	if eventType == "session.error" && sessionID == "" {
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

// opencodeSessionID finds the session an OpenCode event belongs to. Current
// releases put it at properties.sessionID on every session, message, and
// permission event; older ones nested it in the message part or the session
// info, so those places are checked too.
func opencodeSessionID(payload map[string]any) string {
	props, _ := payload["properties"].(map[string]any)
	if id := stringFromMap(props, "sessionID"); id != "" {
		return id
	}
	if part, ok := props["part"].(map[string]any); ok {
		if id := stringFromMap(part, "sessionID"); id != "" {
			return id
		}
	}
	if info, ok := props["info"].(map[string]any); ok {
		if id := stringFromMap(info, "id"); id != "" {
			return id
		}
	}
	return stringFromMap(payload, "sessionID")
}

// removeSessionRecord deletes a session's state record when the session ends.
func removeSessionRecord(agent, sessionID string, stderr io.Writer) {
	identity := sessionIdentity{
		Agent:     agent,
		SessionID: sessionID,
		PaneID:    os.Getenv("TMUX_PANE"),
		Socket:    tmuxSocket(),
	}
	key := sessionKey(identity.Agent, identity.SessionID, identity.PaneID, identity.Socket)
	if err := os.Remove(stateFile(key)); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "ai-attn: warning: failed to remove state file: %v\n", err)
	}
	if err := removeCompatibleLegacyRecord(identity, key); err != nil {
		fmt.Fprintf(stderr, "ai-attn: warning: failed to remove legacy state file: %v\n", err)
	}
}

// matchOpencodeEvent maps an OpenCode event type to an ai-attn state and reason.
//
// file.edited is not mapped: it carries no session ID, so its writes landed
// on a separate pane-keyed record that no later event ever finished.
// message.part.updated already covers tool activity. session.created is not
// mapped either; opening a session is not work, and the first prompt sets
// working through session.status.
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

	case "permission.asked":
		return "waiting", "permission.asked", true
	case "permission.updated": // the name before permission.asked
		return "waiting", "permission.updated", true
	case "permission.replied":
		return "working", "permission.replied", true

	// The question tool blocks the turn on the user's answer.
	case "question.asked":
		return "waiting", "question.asked", true
	case "question.replied", "question.rejected":
		return "working", eventType, true

	case "message.part.updated":
		return "working", "message.part.updated", true
	}

	return "", "", false
}

// hookMuse parses a Muse hook JSON payload and writes the corresponding state record.
//
// Muse emits Claude-shaped payloads — the same hook_event_name vocabulary and
// the same session_id/cwd fields — so the parsing mirrors hookClaude. The one
// structural difference is the environment: Muse runs hooks with a scrubbed
// env that drops TMUX and TMUX_PANE, so these records carry no pane_id and no
// socket. Downstream consumers route them by agent and cwd instead.
func hookMuse(hookPayload string, stderr io.Writer) int {
	var payload map[string]any
	if err := json.Unmarshal([]byte(hookPayload), &payload); err != nil {
		return exitOK
	}

	event := stringFromMap(payload, "hook_event_name")
	sessionID := stringFromMap(payload, "session_id")
	cwd := stringFromMap(payload, "cwd")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Muse always supplies a session UUID. The pane fallbacks the codex and
	// opencode handlers use are deliberately absent: TMUX_PANE is never set
	// inside a Muse hook, so they would only ever produce a ppid key tied to
	// Muse's session host rather than the session itself.
	if sessionID == "" {
		return exitOK
	}

	if event == "SessionEnd" {
		identity := sessionIdentity{
			Agent:     "muse",
			SessionID: sessionID,
			PaneID:    os.Getenv("TMUX_PANE"),
			Socket:    tmuxSocket(),
		}
		key := sessionKey(identity.Agent, identity.SessionID, identity.PaneID, identity.Socket)
		if err := os.Remove(stateFile(key)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "ai-attn: warning: failed to remove state file: %v\n", err)
		}
		if err := removeCompatibleLegacyRecord(identity, key); err != nil {
			fmt.Fprintf(stderr, "ai-attn: warning: failed to remove legacy state file: %v\n", err)
		}
		return exitOK
	}

	state, reason, ok := matchMuseEvent(event)
	if !ok {
		return exitOK
	}
	if event == "Notification" {
		// Claude-shaped payloads name the notification kind. Muse sends
		// permission_prompt for an approval and question_prompt when it asks
		// the user a question; both block on the human. elicitation_dialog is
		// Claude's name for the latter. Any other kind (an idle reminder, an
		// auth notice) must not turn a finished turn into "waiting". A
		// payload without the field keeps the plain notification mapping.
		switch kind := stringFromMap(payload, "notification_type"); kind {
		case "":
		case "permission_prompt", "question_prompt", "elicitation_dialog":
			reason = kind
		default:
			return exitOK
		}
	}
	if isMuseBackgroundToolEvent(event, sessionID, payload) {
		return exitOK
	}

	return writeHookState(state, "muse", sessionID, cwd, reason, stderr)
}

// isMuseBackgroundToolEvent reports whether a Muse tool event comes from one
// of Muse's background child sessions rather than the user's session.
//
// Muse runs short-lived reminder sessions alongside each turn and after it
// ends, and their tool calls fire the plugin's tool hooks under their own
// session_id. Those sessions never submit a prompt or stop, so a record
// opened for one stays at "working" until it expires, and pmux routes Muse
// records by agent and cwd, so it shows the user's finished session as
// working. The ones that run after the turn are flagged post_stop. The rest
// are recognised by having no record: every turn of a real session opens
// with UserPromptSubmit, which writes one before any tool runs.
func isMuseBackgroundToolEvent(event, sessionID string, payload map[string]any) bool {
	switch event {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
	default:
		return false
	}
	if postStop, _ := payload["post_stop"].(bool); postStop {
		return true
	}
	key := sessionKey("muse", sessionID, os.Getenv("TMUX_PANE"), tmuxSocket())
	_, err := readRecord(stateFile(key))
	return err != nil
}

// matchMuseEvent maps a Muse hook event to an ai-attn state and reason.
//
// The mapping follows Muse's own herdr lifecycle plugin (prompt and tool
// activity mean working, permission and notification dialogs mean the agent
// is blocked on a human, Stop means the turn is over), reconciled with the
// state names hookClaude already uses for the identical event vocabulary.
//
// PreToolUse is intentionally not wired in the plugin manifest: PostToolUse
// already provides the working heartbeat, and hooking both would double the
// process spawns per tool call for no additional signal.
//
// As with Claude, the in-turn hooks (UserPromptSubmit, PostToolUse) are
// synchronous in the manifest so their writes finish before Stop; the rest
// stay async. isLateInTurnWrite is the backstop if one still lands late.
func matchMuseEvent(event string) (state, reason string, ok bool) {
	switch event {
	case "UserPromptSubmit", "PreToolUse", "PostToolUse":
		return "working", event, true
	case "PermissionRequest":
		return "waiting", "permission_request", true
	case "Notification":
		return "waiting", "notification", true
	case "Stop":
		return "done", "agent-turn-complete", true
	case "PostToolUseFailure", "StopFailure":
		return "stopped", event, true
	}
	return "", "", false
}

// matchCodexEvent maps a Codex event string to an ai-attn state using substring matching.
func matchCodexEvent(event string) (state string, ok bool) {
	lower := strings.ToLower(event)
	for _, pattern := range []string{"turn-start", "start-turn", "user_prompt", "user-prompt", "userpromptsubmit", "user_message", "user-message", "submit", "resume", "continue", "pretooluse", "posttooluse"} {
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
		Socket:    tmuxSocket(),
	}

	if isLateInTurnWrite(identity, reason) {
		return exitOK
	}

	if _, err := writeStateRecord(identity, state, reason); err != nil {
		fmt.Fprintf(stderr, "ai-attn: failed to write state record: %v\n", err)
		return exitError
	}

	return exitOK
}

// isLateInTurnWrite reports whether an in-turn "working" write would
// overwrite a record that already marks the turn as over.
//
// Claude and Muse tool-use hooks only fire inside a turn, and every new turn
// opens with UserPromptSubmit. OpenCode's message.part.updated likewise only
// fires inside a turn, which opens with session.status busy. A write of that
// kind landing on a turn-end record is a stale write from a hook that ran
// slower than the turn's end (OpenCode does not await its plugin event
// handlers, so their hook processes can finish out of order). Applying it
// would leave a finished session stuck at "working" with no later event to
// correct it, so it is dropped instead.
func isLateInTurnWrite(identity sessionIdentity, reason string) bool {
	var turnEnded func(reason string) bool
	switch identity.Agent {
	case "claude", "muse":
		if reason != "PreToolUse" && reason != "PostToolUse" {
			return false
		}
		turnEnded = func(r string) bool { return r == "agent-turn-complete" || r == "StopFailure" }
	case "opencode":
		if reason != "message.part.updated" {
			return false
		}
		turnEnded = func(r string) bool { return r == "session.status.idle" || r == "session.idle" || r == "session.error" }
	default:
		return false
	}
	key := sessionKey(identity.Agent, identity.SessionID, identity.PaneID, identity.Socket)
	existing, err := readRecord(stateFile(key))
	if err != nil {
		return false
	}
	return turnEnded(existing.Reason)
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
