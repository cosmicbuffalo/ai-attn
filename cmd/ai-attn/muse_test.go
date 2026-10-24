package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHookMuseNotificationTypes verifies a Muse notification only means
// waiting when it is a permission or elicitation dialog, as for Claude.
func TestHookMuseNotificationTypes(t *testing.T) {
	cases := []struct {
		kind  string
		state string
	}{
		{"permission_prompt", "state=waiting"},
		{"question_prompt", "state=waiting"},
		{"elicitation_dialog", "state=waiting"},
		{"idle_prompt", ""},
		{"auth_success", ""},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			withTempHome(t)
			var stdout, stderr bytes.Buffer
			payload := `{"hook_event_name":"Notification","notification_type":"` + tc.kind + `","session_id":"muse-n","cwd":"/work"}`
			if rc := run([]string{"hook", "--agent", "muse"}, strings.NewReader(payload), &stdout, &stderr); rc != exitOK {
				t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
			}
			_, out, _ := runCLI(t, "status", "--agent", "muse", "--session-id", "muse-n")
			if tc.state == "" {
				if strings.Contains(out, "state=waiting") {
					t.Fatalf("%s notification must not set waiting: %s", tc.kind, out)
				}
				return
			}
			if !strings.Contains(out, tc.state) || !strings.Contains(out, tc.kind) {
				t.Fatalf("want %s with reason %s, got %s", tc.state, tc.kind, out)
			}
		})
	}
}

// TestHookMuseIdleNotificationKeepsDone verifies an idle reminder after Stop
// leaves the finished turn at done.
func TestHookMuseIdleNotificationKeepsDone(t *testing.T) {
	withTempHome(t)
	for _, payload := range []string{
		`{"hook_event_name":"Stop","session_id":"muse-i","cwd":"/work"}`,
		`{"hook_event_name":"Notification","notification_type":"idle_prompt","session_id":"muse-i","cwd":"/work"}`,
	} {
		var stdout, stderr bytes.Buffer
		if rc := run([]string{"hook", "--agent", "muse"}, strings.NewReader(payload), &stdout, &stderr); rc != exitOK {
			t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
		}
	}
	_, out, _ := runCLI(t, "status", "--agent", "muse", "--session-id", "muse-i")
	if !strings.Contains(out, "state=done") {
		t.Fatalf("expected done after an idle notification, got %s", out)
	}
}

// TestMusePluginInTurnHooksAreSync verifies the bundled manifest runs the
// in-turn hooks synchronously, so their writes cannot land after Stop, and
// leaves every other hook async.
func TestMusePluginInTurnHooksAreSync(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "plugins", "muse", ".muse-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Capabilities struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	sync := map[string]bool{"UserPromptSubmit": true, "PostToolUse": true}
	seen := map[string]bool{}
	for _, hook := range manifest.Capabilities.Hooks {
		event, _ := hook["event"].(string)
		seen[event] = true
		_, hasAsync := hook["async"]
		if sync[event] && hasAsync {
			t.Fatalf("expected %s hook to be synchronous, got async=%v", event, hook["async"])
		}
		if !sync[event] && hook["async"] != true {
			t.Fatalf("expected %s hook to be async", event)
		}
	}
	for event := range sync {
		if !seen[event] {
			t.Fatalf("manifest does not wire %s", event)
		}
	}
}

func museHook(t *testing.T, payload string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if rc := run([]string{"hook", "--agent", "muse"}, strings.NewReader(payload), &stdout, &stderr); rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
}

// TestHookMuseIgnoresBackgroundSessionToolUse verifies the tool hooks of
// Muse's background reminder sessions, which run under their own session ID
// during and after a turn and never stop, write no record. The payloads are
// shaped like ones captured from Muse 1.4.12.
func TestHookMuseIgnoresBackgroundSessionToolUse(t *testing.T) {
	home := withTempHome(t)
	museHook(t, `{"hook_event_name":"UserPromptSubmit","session_id":"main","turn_id":"turn-1","cwd":"/work"}`)
	museHook(t, `{"hook_event_name":"PostToolUse","session_id":"reminder-1","turn_id":"reminder-1","tool_name":"submit_reminder_decision","cwd":"/work"}`)
	museHook(t, `{"hook_event_name":"Stop","session_id":"main","turn_id":"turn-1","cwd":"/work"}`)
	museHook(t, `{"hook_event_name":"PostToolUse","session_id":"reminder-2","turn_id":"reminder-2","tool_name":"submit_reminder_decision","post_stop":true,"cwd":"/work"}`)

	entries, _ := os.ReadDir(filepath.Join(home, ".local", "state", "ai-attn"))
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(home, ".local", "state", "ai-attn", e.Name()))
		if strings.Contains(string(data), "reminder-") {
			t.Fatalf("a background session wrote a record: %s", data)
		}
	}
	if _, out, _ := runCLI(t, "status", "--agent", "muse", "--session-id", "main"); !strings.Contains(out, "state=done") {
		t.Fatalf("the user's session should be done, got %s", out)
	}
}

// TestHookMuseToolUseAfterPromptSetsWorking verifies the user's own session
// still reports tool activity once its prompt opened the turn.
func TestHookMuseToolUseAfterPromptSetsWorking(t *testing.T) {
	withTempHome(t)
	museHook(t, `{"hook_event_name":"UserPromptSubmit","session_id":"main","turn_id":"turn-1","cwd":"/work"}`)
	museHook(t, `{"hook_event_name":"PermissionRequest","session_id":"main","turn_id":"turn-1","cwd":"/work"}`)
	museHook(t, `{"hook_event_name":"PostToolUse","session_id":"main","turn_id":"turn-1","tool_name":"bash","cwd":"/work"}`)
	_, out, _ := runCLI(t, "status", "--agent", "muse", "--session-id", "main")
	if !strings.Contains(out, "state=working") || !strings.Contains(out, "PostToolUse") {
		t.Fatalf("expected working from the tool use, got %s", out)
	}
}
