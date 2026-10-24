package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func opencodeHook(t *testing.T, payload string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if rc := run([]string{"hook", "--agent", "opencode"}, strings.NewReader(payload), &stdout, &stderr); rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
}

func opencodeStatus(t *testing.T, session string) string {
	t.Helper()
	_, out, _ := runCLI(t, "status", "--agent", "opencode", "--session-id", session)
	return out
}

// TestHookOpencodePermissionAsked verifies current OpenCode's permission
// event, which replaced permission.updated, sets waiting.
func TestHookOpencodePermissionAsked(t *testing.T) {
	withTempHome(t)
	opencodeHook(t, `{"type":"permission.asked","properties":{"id":"per_1","sessionID":"oc-p","permission":"bash","patterns":["git push"]}}`)
	if out := opencodeStatus(t, "oc-p"); !strings.Contains(out, "state=waiting") {
		t.Fatalf("expected waiting, got %s", out)
	}
}

// TestHookOpencodeLatePartUpdateAfterIdleIsIgnored verifies a
// message.part.updated write landing after the turn's idle leaves it done.
func TestHookOpencodeLatePartUpdateAfterIdleIsIgnored(t *testing.T) {
	for _, idle := range []string{
		`{"type":"session.status","properties":{"sessionID":"oc-l","status":{"type":"idle"}}}`,
		`{"type":"session.idle","properties":{"sessionID":"oc-l"}}`,
	} {
		withTempHome(t)
		opencodeHook(t, `{"type":"session.status","properties":{"sessionID":"oc-l","status":{"type":"busy"}}}`)
		opencodeHook(t, idle)
		opencodeHook(t, `{"type":"message.part.updated","properties":{"sessionID":"oc-l","part":{"sessionID":"oc-l"},"time":1}}`)
		if out := opencodeStatus(t, "oc-l"); !strings.Contains(out, "state=done") {
			t.Fatalf("a late part update after %s must not reopen the turn, got %s", idle, out)
		}
	}
}

// TestHookOpencodeNewTurnAfterIdleSetsWorking verifies session.status busy
// opens a new turn, after which part updates apply again.
func TestHookOpencodeNewTurnAfterIdleSetsWorking(t *testing.T) {
	withTempHome(t)
	opencodeHook(t, `{"type":"session.status","properties":{"sessionID":"oc-n","status":{"type":"idle"}}}`)
	opencodeHook(t, `{"type":"session.status","properties":{"sessionID":"oc-n","status":{"type":"busy"}}}`)
	opencodeHook(t, `{"type":"message.part.updated","properties":{"sessionID":"oc-n","time":1}}`)
	out := opencodeStatus(t, "oc-n")
	if !strings.Contains(out, "state=working") || !strings.Contains(out, "message.part.updated") {
		t.Fatalf("expected working from the part update, got %s", out)
	}
}

// TestHookOpencodeSessionDeletedRemovesRecord verifies a deleted session's
// record is removed rather than left at done.
func TestHookOpencodeSessionDeletedRemovesRecord(t *testing.T) {
	home := withTempHome(t)
	opencodeHook(t, `{"type":"session.status","properties":{"sessionID":"oc-d","status":{"type":"busy"}}}`)
	stateDir := filepath.Join(home, ".local", "state", "ai-attn")
	if entries, _ := os.ReadDir(stateDir); len(entries) == 0 {
		t.Fatalf("expected a record in %s before the delete", stateDir)
	}
	opencodeHook(t, `{"type":"session.deleted","properties":{"sessionID":"oc-d","info":{"id":"oc-d"}}}`)
	entries, _ := os.ReadDir(filepath.Join(home, ".local", "state", "ai-attn"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			data, _ := os.ReadFile(filepath.Join(home, ".local", "state", "ai-attn", e.Name()))
			if strings.Contains(string(data), "oc-d") {
				t.Fatalf("record for the deleted session remains: %s", data)
			}
		}
	}
}

// TestHookOpencodeFileEditedWritesNothing verifies file.edited, which has no
// session ID, no longer creates a pane-keyed record that nothing finishes.
func TestHookOpencodeFileEditedWritesNothing(t *testing.T) {
	home := withTempHome(t)
	opencodeHook(t, `{"type":"file.edited","properties":{"file":"/work/main.go"}}`)
	entries, _ := os.ReadDir(filepath.Join(home, ".local", "state", "ai-attn"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Fatalf("file.edited wrote a record: %s", e.Name())
		}
	}
}

func TestOpencodeSessionID(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"current", map[string]any{"properties": map[string]any{"sessionID": "a"}}, "a"},
		{"nested in part", map[string]any{"properties": map[string]any{"part": map[string]any{"sessionID": "b"}}}, "b"},
		{"session info", map[string]any{"properties": map[string]any{"info": map[string]any{"id": "c"}}}, "c"},
		{"top level", map[string]any{"sessionID": "d"}, "d"},
		{"none", map[string]any{"properties": map[string]any{"file": "/x"}}, ""},
	}
	for _, tc := range cases {
		if got := opencodeSessionID(tc.payload); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestHookOpencodeQuestionSetsWaitingUntilAnswered verifies the question
// tool, which blocks the turn on the user, shows as waiting until answered.
func TestHookOpencodeQuestionSetsWaitingUntilAnswered(t *testing.T) {
	for _, answer := range []string{"question.replied", "question.rejected"} {
		withTempHome(t)
		opencodeHook(t, `{"type":"question.asked","properties":{"id":"que_1","sessionID":"oc-q"}}`)
		if out := opencodeStatus(t, "oc-q"); !strings.Contains(out, "state=waiting") {
			t.Fatalf("expected waiting while the question is open, got %s", out)
		}
		opencodeHook(t, `{"type":"`+answer+`","properties":{"requestID":"que_1","sessionID":"oc-q"}}`)
		if out := opencodeStatus(t, "oc-q"); !strings.Contains(out, "state=working") {
			t.Fatalf("expected working after %s, got %s", answer, out)
		}
	}
}

// TestHookOpencodeSessionlessErrorWritesNothing verifies a session.error
// published outside any session (a skill that fails to load) does not open
// a pane-keyed record that nothing would ever clear.
func TestHookOpencodeSessionlessErrorWritesNothing(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("TMUX_PANE", "%172")
	opencodeHook(t, `{"type":"session.error","properties":{"error":{"name":"UnknownError","data":{"message":"bad skill"}}}}`)
	entries, _ := os.ReadDir(filepath.Join(home, ".local", "state", "ai-attn"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Fatalf("a sessionless error wrote a record: %s", e.Name())
		}
	}
}
