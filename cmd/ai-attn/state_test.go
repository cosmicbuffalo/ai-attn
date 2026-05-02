package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSetStateAndStatus verifies that set-state writes a record that status reports as waiting.
func TestSetStateAndStatus(t *testing.T) {
	withTempHome(t)
	rc, _, _ := runCLI(t, "set-state", "--agent", "codex", "--cwd", "/tmp/project", "--session-id", "abc", "--pane-id", "%1", "--state", "waiting", "--reason", "permission_prompt")
	if rc != exitOK {
		t.Fatalf("set-state rc=%d", rc)
	}

	rc, stdout, _ := runCLI(t, "status", "--agent", "codex", "--cwd", "/tmp/project", "--session-id", "abc", "--pane-id", "%1")
	if rc != exitError {
		t.Fatalf("status rc=%d output=%s", rc, stdout)
	}
	if !strings.Contains(stdout, "state=waiting") {
		t.Fatalf("unexpected status output: %s", stdout)
	}
}

// TestClearStateTransitionsToNotWaiting verifies that clear-state removes the waiting state.
func TestClearStateTransitionsToNotWaiting(t *testing.T) {
	withTempHome(t)
	_, _, _ = runCLI(t, "set-state", "--agent", "claude", "--cwd", "/work", "--session-id", "sid-1", "--pane-id", "%2", "--state", "waiting", "--reason", "permission_request")
	rc, _, _ := runCLI(t, "clear-state", "--agent", "claude", "--cwd", "/work", "--session-id", "sid-1", "--pane-id", "%2", "--reason", "Stop")
	if rc != exitOK {
		t.Fatalf("clear-state rc=%d", rc)
	}
	rc, stdout, _ := runCLI(t, "status", "--agent", "claude", "--cwd", "/work", "--session-id", "sid-1", "--pane-id", "%2")
	if rc != exitOK || strings.Contains(stdout, "state=waiting") {
		t.Fatalf("unexpected status rc=%d output=%s", rc, stdout)
	}
}

// TestStatusShowsWaitingRegardlessOfAge verifies that status still reports waiting even for old records within GC window.
func TestStatusShowsWaitingRegardlessOfAge(t *testing.T) {
	withTempHome(t)
	_, _, _ = runCLI(t, "set-state", "--agent", "codex", "--cwd", "/tmp", "--session-id", "s-1", "--state", "waiting", "--reason", "permission_prompt")
	key := sessionKey("codex", "s-1", "")
	path := stateFile(key)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	// Use a timestamp old enough to demonstrate age display but within the GC TTL window.
	record["updated_at"] = float64(time.Now().Unix() - 1000)
	out, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	rc, stdout, _ := runCLI(t, "status", "--key", key, "--agent", "codex")
	if rc != exitError {
		t.Fatalf("expected rc=1 (waiting), got %d; output=%s", rc, stdout)
	}
	if !strings.Contains(stdout, "state=waiting") {
		t.Fatalf("expected state=waiting in output: %s", stdout)
	}
}

// TestGCRemovesStaleState verifies that gc removes state files older than the TTL.
func TestGCRemovesStaleState(t *testing.T) {
	withTempHome(t)
	if err := ensureStateDir(); err != nil {
		t.Fatal(err)
	}
	key := sessionKey("codex", "sid", "")
	record := Record{
		Agent:      "codex",
		SessionKey: key,
		State:      "working",
		UpdatedAt:  1,
		CWD:        "/tmp",
		SessionID:  "sid",
	}
	if err := writeJSON(stateFile(key), record); err != nil {
		t.Fatal(err)
	}
	rc, stdout, _ := runCLI(t, "gc")
	if rc != exitOK || !strings.Contains(stdout, "removed=1") {
		t.Fatalf("unexpected gc rc=%d output=%s", rc, stdout)
	}
	if _, err := os.Stat(stateFile(key)); !os.IsNotExist(err) {
		t.Fatalf("expected state file removed, err=%v", err)
	}
}

// TestListRunsGCWhenEnabled verifies that the list command triggers GC and removes stale files.
func TestListRunsGCWhenEnabled(t *testing.T) {
	home := withTempHome(t)
	cfg := filepath.Join(home, ".config", "ai-attn", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("ttl_seconds = 600\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureStateDir(); err != nil {
		t.Fatal(err)
	}

	key := sessionKey("codex", "sid", "")
	record := Record{
		Agent:      "codex",
		SessionKey: key,
		State:      "working",
		UpdatedAt:  1,
		CWD:        "/tmp",
		SessionID:  "sid",
	}
	if err := writeJSON(stateFile(key), record); err != nil {
		t.Fatal(err)
	}

	rc, _, stderr := runCLI(t, "list")
	if rc != exitOK {
		t.Fatalf("list rc=%d stderr=%s", rc, stderr)
	}
	if _, err := os.Stat(stateFile(key)); !os.IsNotExist(err) {
		t.Fatalf("expected gc to remove state file, err=%v", err)
	}
}

// TestSetStateRequiresStateFlag verifies that set-state without --state exits rc=2.
func TestSetStateRequiresStateFlag(t *testing.T) {
	withTempHome(t)
	rc, _, stderr := runCLI(t, "set-state", "--agent", "codex", "--session-id", "abc")
	if rc != exitUsage {
		t.Fatalf("expected rc=2, got %d", rc)
	}
	if !strings.Contains(stderr, "--state is required") {
		t.Fatalf("expected '--state is required' in stderr: %s", stderr)
	}
}

// TestSetStateNoopWhenDisabled verifies that set-state is a no-op when the config has enabled=false.
func TestSetStateNoopWhenDisabled(t *testing.T) {
	home := withTempHome(t)
	cfg := filepath.Join(home, ".config", "ai-attn", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("enabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, _, _ := runCLI(t, "set-state", "--agent", "codex", "--cwd", "/tmp/project", "--session-id", "abc", "--state", "waiting", "--reason", "permission_prompt")
	if rc != exitOK {
		t.Fatalf("set-state rc=%d", rc)
	}
	if _, err := os.Stat(stateFile(sessionKey("codex", "abc", ""))); !os.IsNotExist(err) {
		t.Fatalf("expected no state file, err=%v", err)
	}
}

// TestListJSONNormalizesWaitingState verifies that list --json includes age for each record.
func TestListJSONNormalizesWaitingState(t *testing.T) {
	withTempHome(t)
	_, _, _ = runCLI(t, "set-state", "--agent", "codex", "--cwd", "/tmp/project-a", "--session-id", "abc", "--pane-id", "%1", "--state", "waiting", "--reason", "permission_prompt")
	_, _, _ = runCLI(t, "clear-state", "--agent", "claude", "--cwd", "/tmp/project-b", "--session-id", "def", "--pane-id", "%2", "--reason", "Stop")

	key := sessionKey("codex", "abc", "%1")
	path := stateFile(key)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	// Use a timestamp old enough to show age but within the GC TTL window.
	record["updated_at"] = float64(time.Now().Unix() - 1000)
	out, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}

	rc, stdout, _ := runCLI(t, "list", "--json")
	if rc != exitOK {
		t.Fatalf("list rc=%d", rc)
	}

	var payload listPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(payload.Records))
	}

	bySession := map[string]Record{}
	for _, item := range payload.Records {
		bySession[item.SessionID] = item
	}
	if bySession["abc"].State != "waiting" {
		t.Fatalf("expected state=waiting for abc: %#v", bySession["abc"])
	}
	if bySession["abc"].PaneID != "%1" {
		t.Fatalf("expected pane_id=%%1, got %s", bySession["abc"].PaneID)
	}
	if bySession["def"].State == "waiting" {
		t.Fatalf("expected non-waiting state for def: %#v", bySession["def"])
	}
}

// TestListPlaintextOutputsRecords verifies that the plaintext list output contains agent, state, and cwd.
func TestListPlaintextOutputsRecords(t *testing.T) {
	withTempHome(t)
	_, _, _ = runCLI(t, "set-state", "--agent", "codex", "--cwd", "/tmp/project", "--session-id", "abc", "--pane-id", "%9", "--state", "waiting", "--reason", "permission_prompt")
	rc, stdout, _ := runCLI(t, "list")
	if rc != exitOK {
		t.Fatalf("list rc=%d", rc)
	}
	if !strings.Contains(stdout, "codex") || !strings.Contains(stdout, "waiting") || !strings.Contains(stdout, "/tmp/project") {
		t.Fatalf("unexpected list output: %s", stdout)
	}
}

// --- GC edge cases ---

// TestGCRemovesNonWaitingRecordsAfterTTL verifies that GC removes non-waiting records after TTL.
func TestGCRemovesNonWaitingRecordsAfterTTL(t *testing.T) {
	home := withTempHome(t)
	cfg := filepath.Join(home, ".config", "ai-attn", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("ttl_seconds = 600\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureStateDir(); err != nil {
		t.Fatal(err)
	}
	key := sessionKey("claude", "gc-work", "")
	record := Record{
		Agent:      "claude",
		SessionKey: key,
		State:      "working",
		Reason:     "PreToolUse",
		UpdatedAt:  time.Now().Unix() - 700, // 700s > TTL of 600s
		CWD:        "/work",
		SessionID:  "gc-work",
	}
	if err := writeJSON(stateFile(key), record); err != nil {
		t.Fatal(err)
	}
	rc, stdout, _ := runCLI(t, "gc")
	if rc != exitOK {
		t.Fatalf("gc rc=%d output=%s", rc, stdout)
	}
	if !strings.Contains(stdout, "removed=1") {
		t.Fatalf("expected removed=1, got %s", stdout)
	}
	if _, err := os.Stat(stateFile(key)); !os.IsNotExist(err) {
		t.Fatalf("expected working state file to be removed by GC, err=%v", err)
	}
}

// TestGCSkipsNonJSONFiles verifies that GC ignores non-.json files in the state directory.
func TestGCSkipsNonJSONFiles(t *testing.T) {
	withTempHome(t)
	if err := ensureStateDir(); err != nil {
		t.Fatal(err)
	}
	txtFile := filepath.Join(stateDir(), "notes.txt")
	if err := os.WriteFile(txtFile, []byte("not a state file"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, stdout, _ := runCLI(t, "gc")
	if rc != exitOK {
		t.Fatalf("gc rc=%d output=%s", rc, stdout)
	}
	if !strings.Contains(stdout, "removed=0") {
		t.Fatalf("expected removed=0, got %s", stdout)
	}
	if _, err := os.Stat(txtFile); err != nil {
		t.Fatalf("expected .txt file to survive GC, err=%v", err)
	}
}

// --- Status/clear edge cases ---

// TestStatusMissingStateFile verifies that querying status for a nonexistent session returns rc=0 (not waiting).
func TestStatusMissingStateFile(t *testing.T) {
	withTempHome(t)
	rc, stdout, _ := runCLI(t, "status", "--agent", "claude", "--session-id", "nonexistent-session")
	if rc != exitOK {
		t.Fatalf("expected rc=0, got %d; output=%s", rc, stdout)
	}
	if !strings.Contains(stdout, "state=") {
		t.Fatalf("expected state= in output: %s", stdout)
	}
}

// TestClearSkipsWorkingState verifies that clear only removes waiting/done records, not working ones.
func TestClearSkipsWorkingState(t *testing.T) {
	withTempHome(t)
	if err := ensureStateDir(); err != nil {
		t.Fatal(err)
	}
	// Create a "working" record
	workingKey := sessionKey("claude", "work-1", "")
	workingRecord := Record{
		Agent:      "claude",
		SessionKey: workingKey,
		State:      "working",
		Reason:     "PreToolUse",
		UpdatedAt:  time.Now().Unix(),
		CWD:        "/work",
		SessionID:  "work-1",
	}
	if err := writeJSON(stateFile(workingKey), workingRecord); err != nil {
		t.Fatal(err)
	}
	// Create a "waiting" record
	waitingKey := sessionKey("claude", "wait-1", "")
	waitingRecord := Record{
		Agent:      "claude",
		SessionKey: waitingKey,
		State:      "waiting",
		Reason:     "permission_request",
		UpdatedAt:  time.Now().Unix(),
		CWD:        "/work",
		SessionID:  "wait-1",
	}
	if err := writeJSON(stateFile(waitingKey), waitingRecord); err != nil {
		t.Fatal(err)
	}
	rc, stdout, _ := runCLI(t, "clear")
	if rc != exitOK {
		t.Fatalf("clear rc=%d output=%s", rc, stdout)
	}
	// Working record should survive
	if _, err := os.Stat(stateFile(workingKey)); err != nil {
		t.Fatalf("expected working state file to survive clear, err=%v", err)
	}
	// Waiting record should be removed
	if _, err := os.Stat(stateFile(waitingKey)); !os.IsNotExist(err) {
		t.Fatalf("expected waiting state file to be cleared, err=%v", err)
	}
}
