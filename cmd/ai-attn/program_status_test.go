package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// withFakeTTY points terminalPath at an empty file for the test and returns its path.
func withFakeTTY(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tty")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	prev := terminalPath
	terminalPath = func() string { return path }
	t.Cleanup(func() { terminalPath = prev })
	return path
}

// writeConfig writes the given TOML to the test HOME's ai-attn config.
func writeConfig(t *testing.T, home, toml string) {
	t.Helper()
	cfg := filepath.Join(home, ".config", "ai-attn", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hookReport runs one hook event and returns what it wrote to the fake terminal.
func hookReport(t *testing.T, tty, agent, payload string) string {
	t.Helper()
	if err := os.Truncate(tty, 0); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if rc := run([]string{"hook", "--agent", agent}, strings.NewReader(payload), &stdout, &stderr); rc != exitOK {
		t.Fatalf("hook rc=%d stderr=%s", rc, stderr.String())
	}
	got, err := os.ReadFile(tty)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestProgramStatus(t *testing.T) {
	tests := []struct {
		state, reason, want string
	}{
		{"working", "PreToolUse", "state=working:msg=V29ya2luZw=="},
		{"waiting", "permission_request", "state=blocked:kind=permission:msg=TmVlZHMgcGVybWlzc2lvbg=="},
		{"waiting", "ToolPermission", "state=blocked:kind=permission:msg=TmVlZHMgcGVybWlzc2lvbg=="},
		{"waiting", "permission.updated", "state=blocked:kind=permission:msg=TmVlZHMgcGVybWlzc2lvbg=="},
		{"waiting", "elicitation_dialog", "state=blocked:kind=question:msg=SGFzIGEgcXVlc3Rpb24="},
		{"waiting", "notification", "state=blocked:msg=V2FpdGluZyBmb3IgaW5wdXQ="},
		{"done", "agent-turn-complete", "state=done:msg=VHVybiBjb21wbGV0ZQ=="},
		{"stopped", "StopFailure", "state=error:msg=RmFpbGVk"},
		{"stopped", "session.error", "state=error:msg=RmFpbGVk"},
		{"stopped", "PostToolUseFailure", "state=working:msg=V29ya2luZw=="},
	}
	for _, tt := range tests {
		got, ok := programStatus(tt.state, tt.reason)
		if !ok || got != tt.want {
			t.Errorf("programStatus(%q, %q) = %q, %t; want %q", tt.state, tt.reason, got, ok, tt.want)
		}
	}
	if _, ok := programStatus("bogus", ""); ok {
		t.Error("programStatus accepted an unknown state")
	}
}

func TestEmitsOSC7501(t *testing.T) {
	tests := []struct {
		name  string
		cfg   Config
		agent string
		want  bool
	}{
		{"off", Config{Enabled: true}, "gemini", false},
		{"default emits non-native", Config{Enabled: true, OSC7501: true}, "gemini", true},
		{"default skips native", Config{Enabled: true, OSC7501: true}, "claude", false},
		{"explicit forces native", Config{Enabled: true, OSC7501: true, OSC7501Agents: []string{"claude"}}, "claude", true},
		{"explicit excludes unlisted", Config{Enabled: true, OSC7501: true, OSC7501Agents: []string{"claude"}}, "gemini", false},
		{"empty list emits nothing", Config{Enabled: true, OSC7501: true, OSC7501Agents: []string{}}, "gemini", false},
		{"disabled", Config{OSC7501: true, OSC7501Agents: []string{"gemini"}}, "gemini", false},
	}
	for _, tt := range tests {
		if got := tt.cfg.emitsOSC7501(tt.agent); got != tt.want {
			t.Errorf("%s: emitsOSC7501(%q) = %t, want %t", tt.name, tt.agent, got, tt.want)
		}
	}
}

func TestParseConfigDistinguishesUnsetAndEmptyAgents(t *testing.T) {
	unset, err := parseConfig([]byte("osc7501 = true\n"))
	if err != nil || unset.OSC7501Agents != nil {
		t.Fatalf("unset osc7501_agents: %+v, %v", unset, err)
	}
	empty, err := parseConfig([]byte("osc7501 = true\nosc7501_agents = []\n"))
	if err != nil || empty.OSC7501Agents == nil || len(empty.OSC7501Agents) != 0 {
		t.Fatalf("empty osc7501_agents: %+v, %v", empty, err)
	}
}

func TestDefaultOSC7501AgentsExcludesNative(t *testing.T) {
	for _, agent := range defaultOSC7501Agents() {
		if nativeOSC7501Agents[agent] {
			t.Errorf("default list includes native agent %q", agent)
		}
	}
	for agent := range nativeOSC7501Agents {
		if !slices.Contains(allAgents, agent) {
			t.Errorf("native list names unsupported agent %q", agent)
		}
	}
}

func TestHookEmitsProgramStatusWhenEnabled(t *testing.T) {
	writeConfig(t, withTempHome(t), "osc7501 = true\n")
	tty := withFakeTTY(t)

	tests := []struct {
		payload, want string
	}{
		{`{"hook_event_name":"BeforeAgent","session_id":"osc-1"}`, "\x1b]7501;state=working:msg=V29ya2luZw==:app=gemini\x1b\\"},
		{`{"hook_event_name":"Notification","notification_type":"ToolPermission","session_id":"osc-1"}`, "\x1b]7501;state=blocked:kind=permission:msg=TmVlZHMgcGVybWlzc2lvbg==:app=gemini\x1b\\"},
		{`{"hook_event_name":"AfterAgent","session_id":"osc-1"}`, "\x1b]7501;state=done:msg=VHVybiBjb21wbGV0ZQ==:app=gemini\x1b\\"},
		{`{"hook_event_name":"SessionEnd","session_id":"osc-1"}`, "\x1b]7501;state=clear:app=gemini\x1b\\"},
	}
	for _, tt := range tests {
		if got := hookReport(t, tty, "gemini", tt.payload); got != tt.want {
			t.Errorf("%s: tty got %q, want %q", tt.payload, got, tt.want)
		}
	}
}

func TestHookSkipsNativeAgentByDefault(t *testing.T) {
	writeConfig(t, withTempHome(t), "osc7501 = true\n")
	tty := withFakeTTY(t)

	for _, event := range []string{"UserPromptSubmit", "SessionEnd"} {
		if got := hookReport(t, tty, "claude", `{"hook_event_name":"`+event+`","session_id":"osc-2","cwd":"/work"}`); got != "" {
			t.Errorf("%s: expected no report for native claude, got %q", event, got)
		}
	}
}

func TestHookExplicitAgentsForceNativeAgent(t *testing.T) {
	writeConfig(t, withTempHome(t), "osc7501 = true\nosc7501_agents = [\"claude\"]\n")
	tty := withFakeTTY(t)

	want := "\x1b]7501;state=working:msg=V29ya2luZw==:app=claude\x1b\\"
	if got := hookReport(t, tty, "claude", `{"hook_event_name":"UserPromptSubmit","session_id":"osc-3","cwd":"/work"}`); got != want {
		t.Fatalf("tty got %q, want %q", got, want)
	}
}

func TestHookSkipsProgramStatusByDefault(t *testing.T) {
	withTempHome(t)
	tty := withFakeTTY(t)

	if got := hookReport(t, tty, "gemini", `{"hook_event_name":"BeforeAgent","session_id":"osc-4"}`); got != "" {
		t.Fatalf("expected no report with osc7501 off, got %q", got)
	}
}

func TestHookDoesNotReportWhenDisabled(t *testing.T) {
	home := withTempHome(t)
	writeConfig(t, home, "enabled = false\nosc7501 = true\n")
	tty := withFakeTTY(t)
	lookups := 0
	terminalPath = func() string { lookups++; return tty }
	key := sessionKey("gemini", "disabled-osc", "", "")
	events := []struct {
		payload string
		state   string
	}{
		{`{"hook_event_name":"BeforeAgent","session_id":"disabled-osc"}`, "working"},
		{`{"hook_event_name":"Notification","notification_type":"ToolPermission","session_id":"disabled-osc"}`, "working"},
		{`{"hook_event_name":"AfterAgent","session_id":"disabled-osc"}`, "done"},
	}
	for _, event := range events {
		if got := hookReport(t, tty, "gemini", event.payload); got != "" {
			t.Fatalf("unexpected report %q", got)
		}
		record, err := readRecord(stateFile(key))
		if err != nil || record.State != event.state {
			t.Fatalf("record = %+v, %v; want state %q", record, err, event.state)
		}
	}
	if got := hookReport(t, tty, "gemini", `{"hook_event_name":"SessionEnd","session_id":"disabled-osc"}`); got != "" {
		t.Fatalf("unexpected session-end report %q", got)
	}
	if _, err := readRecord(stateFile(key)); !os.IsNotExist(err) {
		t.Fatalf("record still exists or read failed: %v", err)
	}
	if lookups != 0 {
		t.Fatalf("terminal lookups = %d, want 0", lookups)
	}
}

func TestHookReportsOnlyProgramStatusChanges(t *testing.T) {
	home := withTempHome(t)
	writeConfig(t, home, "osc7501 = true\n")
	tty := withFakeTTY(t)
	lookups := 0
	terminalPath = func() string { lookups++; return tty }
	const session = "osc-heartbeat"
	const prefix = "\x1b]7501;"
	const suffix = ":app=cortex\x1b\\"
	cases := []struct {
		name, payload, body string
		lookupDelta         int
	}{
		{"prompt", `{"hook_event_name":"UserPromptSubmit","session_id":"` + session + `"}`, "state=working:msg=V29ya2luZw==", 1},
		{"pre tool", `{"hook_event_name":"PreToolUse","session_id":"` + session + `"}`, "", 0},
		{"post tool", `{"hook_event_name":"PostToolUse","session_id":"` + session + `","cwd":"/new"}`, "", 0},
		{"tool failure", `{"hook_event_name":"PostToolUseFailure","session_id":"` + session + `"}`, "", 0},
		{"permission", `{"hook_event_name":"PermissionRequest","session_id":"` + session + `"}`, "state=blocked:kind=permission:msg=TmVlZHMgcGVybWlzc2lvbg==", 1},
		{"same permission", `{"hook_event_name":"Notification","notification_type":"permission_prompt","session_id":"` + session + `"}`, "", 0},
		{"question", `{"hook_event_name":"Notification","notification_type":"elicitation_dialog","session_id":"` + session + `"}`, "state=blocked:kind=question:msg=SGFzIGEgcXVlc3Rpb24=", 1},
		{"stop", `{"hook_event_name":"Stop","session_id":"` + session + `"}`, "state=done:msg=VHVybiBjb21wbGV0ZQ==", 1},
		{"late tool", `{"hook_event_name":"PostToolUse","session_id":"` + session + `"}`, "", 0},
		{"next prompt", `{"hook_event_name":"UserPromptSubmit","session_id":"` + session + `"}`, "state=working:msg=V29ya2luZw==", 1},
	}
	key := sessionKey("cortex", session, "", "")
	for i, tc := range cases {
		if i == 2 {
			record, err := readRecord(stateFile(key))
			if err != nil {
				t.Fatal(err)
			}
			record.UpdatedAt = 1
			if err := writeJSON(stateFile(key), record); err != nil {
				t.Fatal(err)
			}
		}
		before := lookups
		got := hookReport(t, tty, "cortex", tc.payload)
		want := ""
		if tc.body != "" {
			want = prefix + tc.body + suffix
		}
		if got != want {
			t.Errorf("%s report = %q, want %q", tc.name, got, want)
		}
		if delta := lookups - before; delta != tc.lookupDelta {
			t.Errorf("%s lookup delta = %d, want %d", tc.name, delta, tc.lookupDelta)
		}
		if tc.name == "post tool" {
			record, err := readRecord(stateFile(key))
			if err != nil || record.State != "working" || record.Reason != "PostToolUse" || record.CWD != "/new" || record.UpdatedAt <= 1 {
				t.Fatalf("post-tool record = %+v, %v", record, err)
			}
		}
		if tc.name == "tool failure" {
			record, _ := readRecord(stateFile(key))
			if record.State != "stopped" {
				t.Fatalf("tool failure state = %q", record.State)
			}
		}
		if tc.name == "late tool" {
			record, _ := readRecord(stateFile(key))
			if record.State != "done" {
				t.Fatalf("late tool state = %q", record.State)
			}
		}
	}
}

func TestHookReportsProgramStatusAfterPaneChange(t *testing.T) {
	writeConfig(t, withTempHome(t), "osc7501 = true\n")
	tty := withFakeTTY(t)
	t.Setenv("TMUX", "/osc-test,1,1")
	t.Setenv("TMUX_PANE", "%1")
	want := "\x1b]7501;state=working:msg=V29ya2luZw==:app=cortex\x1b\\"
	if got := hookReport(t, tty, "cortex", `{"hook_event_name":"UserPromptSubmit","session_id":"osc-pane"}`); got != want {
		t.Fatalf("first report = %q", got)
	}
	t.Setenv("TMUX_PANE", "%2")
	if got := hookReport(t, tty, "cortex", `{"hook_event_name":"PreToolUse","session_id":"osc-pane"}`); got != want {
		t.Fatalf("pane-change report = %q", got)
	}
	record, err := readRecord(stateFile(sessionKey("cortex", "osc-pane", "%2", "/osc-test")))
	if err != nil || record.PaneID != "%2" {
		t.Fatalf("new pane record = %+v, %v", record, err)
	}
	t.Setenv("TMUX", "/osc-other,2,1")
	if got := hookReport(t, tty, "cortex", `{"hook_event_name":"PostToolUse","session_id":"osc-pane"}`); got != want {
		t.Fatalf("server-change report = %q", got)
	}
}

func TestHookSkipsSupersededProgramStatus(t *testing.T) {
	for _, mode := range []string{"changed", "cleared"} {
		t.Run(mode, func(t *testing.T) {
			writeConfig(t, withTempHome(t), "osc7501 = true\n")
			tty := withFakeTTY(t)
			identity := sessionIdentity{Agent: "cortex", SessionID: "superseded-" + mode}
			if mode == "cleared" {
				if got := hookReport(t, tty, "cortex", `{"hook_event_name":"Stop","session_id":"`+identity.SessionID+`"}`); got == "" {
					t.Fatal("ordinary stop did not report")
				}
			}
			lookups := 0
			terminalPath = func() string {
				lookups++
				if mode == "changed" {
					if _, err := writeStateRecord(identity, "done", "agent-turn-complete"); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := writeStateRecord(identity, "working", "UserPromptSubmit"); err != nil {
						t.Fatal(err)
					}
				}
				return tty
			}
			payload := `{"hook_event_name":"PermissionRequest","session_id":"` + identity.SessionID + `"}`
			if mode == "cleared" {
				payload = `{"hook_event_name":"SessionEnd","session_id":"` + identity.SessionID + `"}`
			}
			if got := hookReport(t, tty, "cortex", payload); got != "" {
				t.Fatalf("superseded report = %q", got)
			}
			if lookups != 1 {
				t.Fatalf("lookup count = %d, want 1", lookups)
			}
			record, err := readRecord(stateFile(sessionKey(identity.Agent, identity.SessionID, "", "")))
			if err != nil {
				t.Fatalf("replacement record missing: %v", err)
			}
			want := "done"
			if mode == "cleared" {
				want = "working"
			}
			if record.State != want {
				t.Fatalf("replacement state = %q, want %q", record.State, want)
			}
		})
	}
}

func TestHookPreservesCopilotLateNotificationGuard(t *testing.T) {
	writeConfig(t, withTempHome(t), "osc7501 = true\nosc7501_agents = [\"copilot\"]\n")
	tty := withFakeTTY(t)
	if got := hookReport(t, tty, "copilot", `{"hook_event_name":"userPromptSubmitted","sessionId":"osc-delayed"}`); got == "" {
		t.Fatal("prompt report missing")
	}
	hookReport(t, tty, "copilot", `{"hook_event_name":"agentStop","sessionId":"osc-delayed"}`)
	if got := hookReport(t, tty, "copilot", `{"hook_event_name":"notification","notification_type":"permission_prompt","sessionId":"osc-delayed"}`); got != "" {
		t.Fatalf("late notification report = %q", got)
	}
	record, err := readRecord(stateFile(sessionKey("copilot", "osc-delayed", "", "")))
	if err != nil || record.State != "done" {
		t.Fatalf("record = %+v, %v", record, err)
	}
}

func TestHookWithoutTerminalStillRecordsState(t *testing.T) {
	writeConfig(t, withTempHome(t), "osc7501 = true\n")
	prev := terminalPath
	terminalPath = func() string { return "" }
	t.Cleanup(func() { terminalPath = prev })

	var stdout, stderr bytes.Buffer
	rc := run([]string{"hook", "--agent", "gemini"}, strings.NewReader(`{"hook_event_name":"AfterAgent","session_id":"osc-5"}`), &stdout, &stderr)
	if rc != exitOK || stderr.Len() != 0 {
		t.Fatalf("expected a silent skip: rc=%d stderr=%q", rc, stderr.String())
	}
	rc, out, _ := runCLI(t, "status", "--agent", "gemini", "--session-id", "osc-5")
	if rc != exitOK || !strings.Contains(out, "state=done") {
		t.Fatalf("expected done: rc=%d out=%s", rc, out)
	}
}

func TestDoctorReportsOSC7501Agents(t *testing.T) {
	home := withTempHome(t)

	_, out, _ := runCLI(t, "doctor")
	if want := "osc7501_agents=" + strings.Join(defaultOSC7501Agents(), ",") + " (default)"; !strings.Contains(out, want) {
		t.Fatalf("doctor output missing %q:\n%s", want, out)
	}

	writeConfig(t, home, "osc7501 = true\nosc7501_agents = [\"claude\", \"nope\"]\n")
	_, out, _ = runCLI(t, "doctor")
	for _, want := range []string{"osc7501_agents=claude,nope\n", "osc7501_agents_unknown=nope\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
}
