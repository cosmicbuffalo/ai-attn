package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeHookSetsAndClearsWaiting verifies that the Claude shell hook sets and clears the waiting state end-to-end.
func TestClaudeHookSetsAndClearsWaiting(t *testing.T) {
	home := withTempHome(t)
	bin := buildBinary(t)
	hook := filepath.Join(repoRoot(t), "hooks", "claude.sh")

	env := append(os.Environ(), "HOME="+home, "AI_ATTN_BIN="+bin)
	setPayload := `{"hook_event_name":"Notification","notification_type":"permission_prompt","session_id":"claude-session","cwd":"/tmp/claude"}`
	cmd := exec.Command("bash", hook)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(setPayload)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook set failed: %v\n%s", err, string(out))
	}

	cmd = exec.Command(bin, "status", "--agent", "claude", "--session-id", "claude-session", "--cwd", "/tmp/claude")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected status to exit 1 (waiting) after set: %v\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "state=waiting") {
		t.Fatalf("unexpected status after set: %s", string(out))
	}

	clearPayload := `{"hook_event_name":"UserPromptSubmit","session_id":"claude-session","cwd":"/tmp/claude"}`
	cmd = exec.Command("bash", hook)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(clearPayload)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook clear failed: %v\n%s", err, string(out))
	}

	cmd = exec.Command(bin, "status", "--agent", "claude", "--session-id", "claude-session", "--cwd", "/tmp/claude")
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected status to exit 0 after clear: %v\n%s", err, string(out))
	}
	if strings.Contains(string(out), "state=waiting") {
		t.Fatalf("unexpected state=waiting after clear: %s", string(out))
	}
}

// TestHookWaitingReasonChangeRefreshesTimestamp verifies that a reason change while already waiting refreshes updated_at.
func TestHookWaitingReasonChangeRefreshesTimestamp(t *testing.T) {
	withTempHome(t)
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(`{"hook_event_name":"PermissionRequest","session_id":"r1","cwd":"/work"}`), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	key := sessionKey("claude", "r1", "")
	path := stateFile(key)
	record, err := readRecord(path)
	if err != nil {
		t.Fatal(err)
	}
	record.UpdatedAt = 1
	if err := writeJSON(path, record); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	rc = run([]string{"hook", "--agent", "claude"}, strings.NewReader(`{"hook_event_name":"Elicitation","session_id":"r1","cwd":"/work"}`), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	updated, err := readRecord(path)
	if err != nil {
		t.Fatal(err)
	}
	if updated.UpdatedAt == 1 {
		t.Fatalf("expected updated_at refresh on reason change: %#v", updated)
	}
	if updated.Reason != "elicitation" {
		t.Fatalf("expected updated reason, got %#v", updated)
	}
}

// TestHookClaudeSetAndClear verifies that the Claude hook in-process path sets and clears waiting state.
func TestHookClaudeSetAndClear(t *testing.T) {
	withTempHome(t)
	payload := `{"hook_event_name":"Notification","notification_type":"permission_prompt","session_id":"s1","cwd":"/work"}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook set rc=%d stderr=%s", rc, stderr.String())
	}

	rc, statusOut, _ := runCLI(t, "status", "--agent", "claude", "--session-id", "s1", "--cwd", "/work")
	if rc != exitError || !strings.Contains(statusOut, "state=waiting") {
		t.Fatalf("expected waiting after hook set: rc=%d out=%s", rc, statusOut)
	}

	stdout.Reset()
	stderr.Reset()
	clearPayload := `{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":"/work"}`
	rc = run([]string{"hook", "--agent", "claude"}, strings.NewReader(clearPayload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook clear rc=%d stderr=%s", rc, stderr.String())
	}

	rc, statusOut, _ = runCLI(t, "status", "--agent", "claude", "--session-id", "s1", "--cwd", "/work")
	if rc != exitOK || strings.Contains(statusOut, "state=waiting") {
		t.Fatalf("expected not waiting after hook clear: rc=%d out=%s", rc, statusOut)
	}
}

// TestHookCodexSetAndClear verifies that the Codex hook sets waiting on permission_required and clears on turn-start.
func TestHookCodexSetAndClear(t *testing.T) {
	withTempHome(t)
	t.Setenv("CODEX_SESSION_ID", "cx-1")

	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "codex"}, strings.NewReader("permission_required"), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook set rc=%d stderr=%s", rc, stderr.String())
	}

	rc, statusOut, _ := runCLI(t, "status", "--agent", "codex", "--session-id", "cx-1")
	if rc != exitError || !strings.Contains(statusOut, "state=waiting") {
		t.Fatalf("expected waiting: rc=%d out=%s", rc, statusOut)
	}

	stdout.Reset()
	stderr.Reset()
	rc = run([]string{"hook", "--agent", "codex"}, strings.NewReader("turn-start"), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook clear rc=%d stderr=%s", rc, stderr.String())
	}

	rc, statusOut, _ = runCLI(t, "status", "--agent", "codex", "--session-id", "cx-1")
	if rc != exitOK || strings.Contains(statusOut, "state=waiting") {
		t.Fatalf("expected not waiting: rc=%d out=%s", rc, statusOut)
	}
}

// TestHookCodexJSON verifies that Codex hooks correctly parse JSON payloads with thread-id and event type.
func TestHookCodexJSON(t *testing.T) {
	withTempHome(t)

	payload := `{"type":"permission_required","thread-id":"t1","cwd":"/code"}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "codex"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}

	rc, statusOut, _ := runCLI(t, "status", "--agent", "codex", "--session-id", "t1", "--cwd", "/code")
	if rc != exitError || !strings.Contains(statusOut, "state=waiting") {
		t.Fatalf("expected waiting: rc=%d out=%s", rc, statusOut)
	}
}

// TestHookCodexHooksEnginePayload verifies that Codex hooks handle the hooks-engine JSON format with hook_event_name.
func TestHookCodexHooksEnginePayload(t *testing.T) {
	withTempHome(t)

	payload := `{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":"/code","prompt":"hello"}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "codex"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}

	_, statusOut, _ := runCLI(t, "status", "--agent", "codex", "--session-id", "s1", "--cwd", "/code")
	if !strings.Contains(statusOut, "state=working") {
		t.Fatalf("expected working: out=%s", statusOut)
	}
}

// TestHookCodexUnknownEventIsNoop verifies that an unrecognized Codex event does not create a state file.
func TestHookCodexUnknownEventIsNoop(t *testing.T) {
	withTempHome(t)
	t.Setenv("CODEX_SESSION_ID", "cx-2")

	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "codex"}, strings.NewReader("some-unknown-event"), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d", rc)
	}

	key := sessionKey("codex", "cx-2", "")
	if _, err := os.Stat(stateFile(key)); !os.IsNotExist(err) {
		t.Fatalf("expected no state file for unknown event, err=%v", err)
	}
}

// TestMatchCodexEvent verifies that matchCodexEvent correctly maps known events to states.
func TestMatchCodexEvent(t *testing.T) {
	tests := []struct {
		event   string
		state   string
		matched bool
	}{
		{"permission_required", "waiting", true},
		{"agent-turn-complete", "done", true},
		{"end_turn", "done", true},
		{"turn-start", "working", true},
		{"user_prompt", "working", true},
		{"submit", "working", true},
		{"resume", "working", true},
		{"something-random", "", false},
		// New hooks engine events (PascalCase)
		{"UserPromptSubmit", "working", true},
		{"PermissionRequest", "waiting", true},
		{"Stop", "done", true},
	}
	for _, tt := range tests {
		s, m := matchCodexEvent(tt.event)
		if s != tt.state || m != tt.matched {
			t.Errorf("matchCodexEvent(%q) = (%v, %v), want (%v, %v)", tt.event, s, m, tt.state, tt.matched)
		}
	}
}

// TestCodexHookSetAndCompleteToDone verifies the Codex shell hook transitions from waiting to done end-to-end.
func TestCodexHookSetAndCompleteToDone(t *testing.T) {
	home := withTempHome(t)
	bin := buildBinary(t)
	hook := filepath.Join(repoRoot(t), "hooks", "codex.sh")

	env := append(os.Environ(), "HOME="+home, "AI_ATTN_BIN="+bin, "CODEX_SESSION_ID=codex-session")
	cmd := exec.Command("bash", hook, "permission_required")
	cmd.Env = env
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook set failed: %v\n%s", err, string(out))
	}

	cmd = exec.Command(bin, "status", "--agent", "codex", "--session-id", "codex-session", "--cwd", repoRoot(t))
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected status to exit 1 (waiting) after set: %v\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "state=waiting") {
		t.Fatalf("unexpected status after set: %s", string(out))
	}

	cmd = exec.Command("bash", hook, "agent-turn-complete")
	cmd.Env = env
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook completion failed: %v\n%s", err, string(out))
	}

	cmd = exec.Command(bin, "status", "--agent", "codex", "--session-id", "codex-session", "--cwd", repoRoot(t))
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected status to exit 0 after completion: %v\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "state=done") {
		t.Fatalf("unexpected status after completion: %s", string(out))
	}
}

// --- OpenCode hook tests ---

// TestHookOpencodePermissionUpdated verifies that a permission.updated event sets state=waiting.
func TestHookOpencodePermissionUpdated(t *testing.T) {
	withTempHome(t)
	payload := `{"type":"permission.updated","properties":{"sessionID":"oc-1"}}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "opencode"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	rc, statusOut, _ := runCLI(t, "status", "--agent", "opencode", "--session-id", "oc-1")
	if rc != exitError || !strings.Contains(statusOut, "state=waiting") {
		t.Fatalf("expected waiting: rc=%d out=%s", rc, statusOut)
	}
}

// TestHookOpencodeSessionStatusBusy verifies that session.status with busy type sets state=working.
func TestHookOpencodeSessionStatusBusy(t *testing.T) {
	withTempHome(t)
	payload := `{"type":"session.status","properties":{"sessionID":"oc-2","status":{"type":"busy"}}}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "opencode"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	rc, statusOut, _ := runCLI(t, "status", "--agent", "opencode", "--session-id", "oc-2")
	if rc != exitOK || !strings.Contains(statusOut, "state=working") {
		t.Fatalf("expected working: rc=%d out=%s", rc, statusOut)
	}
}

// TestHookOpencodeSessionStatusIdle verifies that session.status with idle type sets state=done.
func TestHookOpencodeSessionStatusIdle(t *testing.T) {
	withTempHome(t)
	payload := `{"type":"session.status","properties":{"sessionID":"oc-3","status":{"type":"idle"}}}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "opencode"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	rc, statusOut, _ := runCLI(t, "status", "--agent", "opencode", "--session-id", "oc-3")
	if rc != exitOK || !strings.Contains(statusOut, "state=done") {
		t.Fatalf("expected done: rc=%d out=%s", rc, statusOut)
	}
}

// TestHookOpencodeSessionError verifies that session.error sets state=stopped.
func TestHookOpencodeSessionError(t *testing.T) {
	withTempHome(t)
	payload := `{"type":"session.error","properties":{"sessionID":"oc-4"}}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "opencode"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	rc, statusOut, _ := runCLI(t, "status", "--agent", "opencode", "--session-id", "oc-4")
	if rc != exitOK || !strings.Contains(statusOut, "state=stopped") {
		t.Fatalf("expected stopped: rc=%d out=%s", rc, statusOut)
	}
}

// TestHookOpencodePermissionReplied verifies that permission.replied sets state=working.
func TestHookOpencodePermissionReplied(t *testing.T) {
	withTempHome(t)
	payload := `{"type":"permission.replied","properties":{"sessionID":"oc-5"}}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "opencode"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	rc, statusOut, _ := runCLI(t, "status", "--agent", "opencode", "--session-id", "oc-5")
	if rc != exitOK || !strings.Contains(statusOut, "state=working") {
		t.Fatalf("expected working: rc=%d out=%s", rc, statusOut)
	}
}

// TestHookOpencodeUnknownEventIsNoop verifies that an unrecognized OpenCode event does not create a state file.
func TestHookOpencodeUnknownEventIsNoop(t *testing.T) {
	withTempHome(t)
	payload := `{"type":"unknown.event","properties":{"sessionID":"oc-6"}}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "opencode"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	key := sessionKey("opencode", "oc-6", "")
	if _, err := os.Stat(stateFile(key)); !os.IsNotExist(err) {
		t.Fatalf("expected no state file for unknown event, err=%v", err)
	}
}

// --- matchOpencodeEvent unit tests ---

// TestMatchOpencodeEvent is a table-driven test for all OpenCode event types.
func TestMatchOpencodeEvent(t *testing.T) {
	tests := []struct {
		name    string
		event   string
		raw     map[string]any
		state   string
		reason  string
		matched bool
	}{
		{
			name:    "session.status busy",
			event:   "session.status",
			raw:     map[string]any{"properties": map[string]any{"status": map[string]any{"type": "busy"}}},
			state:   "working",
			reason:  "session.status.busy",
			matched: true,
		},
		{
			name:    "session.status idle",
			event:   "session.status",
			raw:     map[string]any{"properties": map[string]any{"status": map[string]any{"type": "idle"}}},
			state:   "done",
			reason:  "session.status.idle",
			matched: true,
		},
		{
			name:    "session.status retry",
			event:   "session.status",
			raw:     map[string]any{"properties": map[string]any{"status": map[string]any{"type": "retry"}}},
			state:   "working",
			reason:  "session.status.retry",
			matched: true,
		},
		{
			name:    "session.idle",
			event:   "session.idle",
			raw:     map[string]any{},
			state:   "done",
			reason:  "session.idle",
			matched: true,
		},
		{
			name:    "session.error",
			event:   "session.error",
			raw:     map[string]any{},
			state:   "stopped",
			reason:  "session.error",
			matched: true,
		},
		{
			name:    "permission.updated",
			event:   "permission.updated",
			raw:     map[string]any{},
			state:   "waiting",
			reason:  "permission.updated",
			matched: true,
		},
		{
			name:    "permission.replied",
			event:   "permission.replied",
			raw:     map[string]any{},
			state:   "working",
			reason:  "permission.replied",
			matched: true,
		},
		{
			name:    "message.part.updated",
			event:   "message.part.updated",
			raw:     map[string]any{},
			state:   "working",
			reason:  "message.part.updated",
			matched: true,
		},
		{
			name:    "file.edited",
			event:   "file.edited",
			raw:     map[string]any{},
			state:   "working",
			reason:  "file.edited",
			matched: true,
		},
		{
			name:    "session.created",
			event:   "session.created",
			raw:     map[string]any{},
			state:   "working",
			reason:  "session.created",
			matched: true,
		},
		{
			name:    "session.deleted",
			event:   "session.deleted",
			raw:     map[string]any{},
			state:   "done",
			reason:  "session.deleted",
			matched: true,
		},
		{
			name:    "unknown",
			event:   "something.unknown",
			raw:     map[string]any{},
			state:   "",
			reason:  "",
			matched: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, reason, ok := matchOpencodeEvent(tt.event, tt.raw)
			if state != tt.state || reason != tt.reason || ok != tt.matched {
				t.Errorf("matchOpencodeEvent(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.event, state, reason, ok, tt.state, tt.reason, tt.matched)
			}
		})
	}
}

// --- Claude hook edge cases ---

// TestHookClaudeSessionEndRemovesState verifies that SessionEnd removes an existing state file.
func TestHookClaudeSessionEndRemovesState(t *testing.T) {
	withTempHome(t)
	// Create a waiting state first
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(`{"hook_event_name":"PermissionRequest","session_id":"se-1","cwd":"/work"}`), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook set rc=%d stderr=%s", rc, stderr.String())
	}
	key := sessionKey("claude", "se-1", "")
	if _, err := os.Stat(stateFile(key)); err != nil {
		t.Fatalf("expected state file to exist after PermissionRequest: %v", err)
	}

	// Fire SessionEnd
	stdout.Reset()
	stderr.Reset()
	rc = run([]string{"hook", "--agent", "claude"}, strings.NewReader(`{"hook_event_name":"SessionEnd","session_id":"se-1","cwd":"/work"}`), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook SessionEnd rc=%d stderr=%s", rc, stderr.String())
	}
	if _, err := os.Stat(stateFile(key)); !os.IsNotExist(err) {
		t.Fatalf("expected state file removed after SessionEnd, err=%v", err)
	}
}

// TestHookClaudeStopFailureSetsStoppedState verifies that StopFailure sets state=stopped.
func TestHookClaudeStopFailureSetsStoppedState(t *testing.T) {
	withTempHome(t)
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(`{"hook_event_name":"StopFailure","session_id":"sf-1","cwd":"/work"}`), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	rc, statusOut, _ := runCLI(t, "status", "--agent", "claude", "--session-id", "sf-1")
	if rc != exitOK || !strings.Contains(statusOut, "state=stopped") {
		t.Fatalf("expected stopped: rc=%d out=%s", rc, statusOut)
	}
}

// TestHookClaudeCWDFallbackToWorkspaceRoots verifies that CWD falls back to workspace_roots when "cwd" is absent.
func TestHookClaudeCWDFallbackToWorkspaceRoots(t *testing.T) {
	withTempHome(t)
	payload := `{"hook_event_name":"PermissionRequest","session_id":"wr-1","workspace_roots":["/fallback"]}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	key := sessionKey("claude", "wr-1", "")
	record, err := readRecord(stateFile(key))
	if err != nil {
		t.Fatalf("failed to read state record: %v", err)
	}
	if record.CWD != "/fallback" {
		t.Fatalf("expected CWD=/fallback, got %q", record.CWD)
	}
}

// TestHookClaudeFallsBackToConversationID verifies that conversation_id is used when session_id is absent.
func TestHookClaudeFallsBackToConversationID(t *testing.T) {
	withTempHome(t)
	payload := `{"hook_event_name":"PermissionRequest","conversation_id":"conv-1","cwd":"/work"}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	key := sessionKey("claude", "conv-1", "")
	record, err := readRecord(stateFile(key))
	if err != nil {
		t.Fatalf("failed to read state record: %v", err)
	}
	if record.SessionID != "conv-1" {
		t.Fatalf("expected session_id=conv-1, got %q", record.SessionID)
	}
}

// TestHookClaudeInvalidJSONIsNoop verifies that invalid JSON input is a no-op.
func TestHookClaudeInvalidJSONIsNoop(t *testing.T) {
	withTempHome(t)
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader("not json"), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("expected rc=0 for invalid JSON, got %d", rc)
	}
	// Verify no state files were created
	dir := stateDir()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Fatalf("expected no state files, found %s", e.Name())
		}
	}
}

// TestHookClaudeUnknownEventIsNoop verifies that an unknown hook_event_name is a no-op.
func TestHookClaudeUnknownEventIsNoop(t *testing.T) {
	withTempHome(t)
	payload := `{"hook_event_name":"SomeNewEvent","session_id":"unk-1","cwd":"/work"}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("expected rc=0 for unknown event, got %d", rc)
	}
	key := sessionKey("claude", "unk-1", "")
	if _, err := os.Stat(stateFile(key)); !os.IsNotExist(err) {
		t.Fatalf("expected no state file for unknown event, err=%v", err)
	}
}

// TestHookClaudeNotificationUnknownTypeIsNoop verifies that a Notification with unknown notification_type is a no-op.
func TestHookClaudeNotificationUnknownTypeIsNoop(t *testing.T) {
	withTempHome(t)
	payload := `{"hook_event_name":"Notification","notification_type":"info","session_id":"ni-1","cwd":"/work"}`
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(payload), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("expected rc=0, got %d", rc)
	}
	key := sessionKey("claude", "ni-1", "")
	if _, err := os.Stat(stateFile(key)); !os.IsNotExist(err) {
		t.Fatalf("expected no state file for info notification, err=%v", err)
	}
}

// --- Hook command validation ---

// TestHookMissingAgentFlag verifies that running hook without --agent exits with rc=2 and shows an error.
func TestHookMissingAgentFlag(t *testing.T) {
	withTempHome(t)
	rc, _, stderr := runCLI(t, "hook")
	if rc != exitUsage {
		t.Fatalf("expected rc=2, got %d", rc)
	}
	if !strings.Contains(stderr, "--agent is required") {
		t.Fatalf("expected '--agent is required' in stderr: %s", stderr)
	}
}

// TestHookUnknownAgent verifies that hook with an unknown agent exits with rc=2.
func TestHookUnknownAgent(t *testing.T) {
	withTempHome(t)
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "gemini"}, strings.NewReader("{}"), &stdout, &stderr)
	if rc != exitUsage {
		t.Fatalf("expected rc=2, got %d", rc)
	}
}

// TestHookEmptyStdinIsNoop verifies that hook with empty stdin exits rc=0.
func TestHookEmptyStdinIsNoop(t *testing.T) {
	withTempHome(t)
	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(""), &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("expected rc=0 for empty stdin, got %d", rc)
	}
}

// TestPermissionClearCycleSameKey verifies that a permission-then-submit cycle for the same session clears the waiting state.
func TestPermissionClearCycleSameKey(t *testing.T) {
	home := withTempHome(t)
	stateDir := filepath.Join(home, ".local", "state", "ai-attn")
	t.Setenv("AI_ATTN_STATE_DIR", stateDir)
	t.Setenv("AI_ATTN_CONFIG", filepath.Join(home, "config.json"))
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"ttl_seconds":600}`), 0o644)

	sid := "test-session-uuid"

	permJSON := `{"hook_event_name":"PermissionRequest","session_id":"` + sid + `","cwd":"/project-a"}`
	var stdout1, stderr1 bytes.Buffer
	rc := run([]string{"hook", "--agent", "claude"}, strings.NewReader(permJSON), &stdout1, &stderr1)
	if rc != exitOK {
		t.Fatalf("hook PermissionRequest failed: rc=%d stderr=%s", rc, stderr1.String())
	}

	rc2, out2, _ := runCLI(t, "list", "--json")
	if rc2 != 0 {
		t.Fatalf("list failed: rc=%d", rc2)
	}
	if !strings.Contains(out2, `"state":"waiting"`) {
		t.Fatalf("expected state=waiting after PermissionRequest: %s", out2)
	}

	submitJSON := `{"hook_event_name":"UserPromptSubmit","session_id":"` + sid + `","cwd":"/project-b"}`
	var stdout3, stderr3 bytes.Buffer
	rc = run([]string{"hook", "--agent", "claude"}, strings.NewReader(submitJSON), &stdout3, &stderr3)
	if rc != exitOK {
		t.Fatalf("hook UserPromptSubmit failed: rc=%d stderr=%s", rc, stderr3.String())
	}

	rc4, out4, _ := runCLI(t, "list", "--json")
	if rc4 != 0 {
		t.Fatalf("list failed: rc=%d", rc4)
	}
	if strings.Contains(out4, `"state":"waiting"`) {
		t.Fatalf("expected no waiting records after UserPromptSubmit from different CWD: %s", out4)
	}
}
