package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hook runs `ai-attn hook --agent agent [--event event]` with payload.
func hook(t *testing.T, agent, event, payload string) {
	t.Helper()
	args := []string{"hook", "--agent", agent}
	if event != "" {
		args = append(args, "--event", event)
	}
	var stdout, stderr bytes.Buffer
	if rc := run(args, strings.NewReader(payload), &stdout, &stderr); rc != exitOK {
		t.Fatalf("hook %s %s: exit %d: %s", agent, event, rc, stderr.String())
	}
}

// record returns the state record for agent's session, or nil.
func record(t *testing.T, agent, sessionID string) *Record {
	t.Helper()
	rec, err := readRecord(stateFile(sessionKey(agent, sessionID, "", "")))
	if err != nil {
		return nil
	}
	return &rec
}

func wantState(t *testing.T, agent, sessionID, state, reason string) {
	t.Helper()
	rec := record(t, agent, sessionID)
	if rec == nil {
		t.Fatalf("%s/%s: no record, want %s", agent, sessionID, state)
	}
	if rec.State != state || (reason != "" && rec.Reason != reason) {
		t.Fatalf("%s/%s: got %s (%s), want %s (%s)", agent, sessionID, rec.State, rec.Reason, state, reason)
	}
}

func wantNoRecord(t *testing.T, agent, sessionID string) {
	t.Helper()
	if rec := record(t, agent, sessionID); rec != nil {
		t.Fatalf("%s/%s: want no record, got %s (%s)", agent, sessionID, rec.State, rec.Reason)
	}
}

// --- hooks ---

func TestHookGeminiLifecycle(t *testing.T) {
	withTempHome(t)
	p := func(event, extra string) string {
		return `{"hook_event_name":"` + event + `","session_id":"g1","cwd":"/work"` + extra + `}`
	}
	hook(t, "gemini", "", p("BeforeAgent", ""))
	wantState(t, "gemini", "g1", "working", "BeforeAgent")
	hook(t, "gemini", "", p("Notification", `,"notification_type":"ToolPermission"`))
	wantState(t, "gemini", "g1", "waiting", "ToolPermission")
	hook(t, "gemini", "", p("AfterTool", ""))
	wantState(t, "gemini", "g1", "working", "AfterTool")
	hook(t, "gemini", "", p("AfterAgent", ""))
	wantState(t, "gemini", "g1", "done", "agent-turn-complete")
	hook(t, "gemini", "", p("Notification", `,"notification_type":"Other"`))
	wantState(t, "gemini", "g1", "done", "")
	hook(t, "gemini", "", p("SessionEnd", `,"reason":"exit"`))
	wantNoRecord(t, "gemini", "g1")
}

func TestHookAgyLifecycle(t *testing.T) {
	withTempHome(t)
	base := `"conversationId":"a1","workspacePaths":["/work/app"]`
	hook(t, "agy", "PreInvocation", `{`+base+`,"invocationNum":0}`)
	wantState(t, "agy", "a1", "working", "PreInvocation")
	if rec := record(t, "agy", "a1"); rec.CWD != "/work/app" {
		t.Fatalf("cwd from workspacePaths: got %q", rec.CWD)
	}
	// Background tasks still running: not back at the prompt yet.
	hook(t, "agy", "Stop", `{`+base+`,"fullyIdle":false,"terminationReason":"NO_TOOL_CALL"}`)
	wantState(t, "agy", "a1", "working", "")
	hook(t, "agy", "Stop", `{`+base+`,"fullyIdle":true,"terminationReason":"NO_TOOL_CALL"}`)
	wantState(t, "agy", "a1", "done", "agent-turn-complete")
	// 1.5.4+ names the event in the payload, so no argument is needed.
	hook(t, "agy", "", `{`+base+`,"hookEventName":"PreInvocation"}`)
	wantState(t, "agy", "a1", "working", "PreInvocation")
}

func TestHookAgyScriptPrintsJSON(t *testing.T) {
	home := withTempHome(t)
	bin := buildBinary(t)
	script := filepath.Join(repoRoot(t), "hooks", "agy.sh")
	for _, binary := range []string{bin, filepath.Join(home, "missing-ai-attn")} {
		cmd := exec.Command("bash", script, "PreInvocation")
		cmd.Env = append(os.Environ(), "HOME="+home, "AI_ATTN_BIN="+binary)
		cmd.Stdin = strings.NewReader(`{"conversationId":"s","workspacePaths":["/w"]}`)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("agy.sh failed: %v", err)
		}
		if strings.TrimSpace(string(out)) != "{}" {
			t.Fatalf("agy.sh must print {} (AI_ATTN_BIN=%s), got %q", binary, out)
		}
	}
}

func TestHookCopilotLifecycle(t *testing.T) {
	withTempHome(t)
	base := `"sessionId":"c1","cwd":"/work","timestamp":1794500000000`
	hook(t, "copilot", "userPromptSubmitted", `{`+base+`,"prompt":"hi"}`)
	wantState(t, "copilot", "c1", "working", "userPromptSubmitted")
	hook(t, "copilot", "notification", `{`+base+`,"hook_event_name":"Notification","notification_type":"permission_prompt"}`)
	wantState(t, "copilot", "c1", "waiting", "permission_prompt")
	hook(t, "copilot", "postToolUse", `{`+base+`,"toolName":"bash"}`)
	wantState(t, "copilot", "c1", "working", "postToolUse")
	hook(t, "copilot", "agentStop", `{`+base+`,"stopReason":"end_turn"}`)
	wantState(t, "copilot", "c1", "done", "agent-turn-complete")
	// notification is fire-and-forget; one landing after agentStop is late.
	hook(t, "copilot", "notification", `{`+base+`,"notification_type":"permission_prompt"}`)
	wantState(t, "copilot", "c1", "done", "")
	hook(t, "copilot", "notification", `{`+base+`,"notification_type":"agent_idle"}`)
	wantState(t, "copilot", "c1", "done", "")
	hook(t, "copilot", "sessionEnd", `{`+base+`,"reason":"user_exit"}`)
	wantNoRecord(t, "copilot", "c1")
}

func TestHookGrokLifecycle(t *testing.T) {
	withTempHome(t)
	p := func(event, extra string) string {
		return `{"hookEventName":"x","hook_event_name":"` + event + `","sessionId":"k1","cwd":"/work"` + extra + `}`
	}
	hook(t, "grok", "", p("UserPromptSubmit", `,"promptId":"p1"`))
	wantState(t, "grok", "k1", "working", "UserPromptSubmit")
	hook(t, "grok", "", p("Notification", `,"notificationType":"permission_prompt"`))
	wantState(t, "grok", "k1", "waiting", "permission_prompt")
	hook(t, "grok", "", p("Stop", `,"reason":"end_turn"`))
	wantState(t, "grok", "k1", "done", "agent-turn-complete")
	// idle_prompt arrives about a minute after every turn; it is not a request.
	hook(t, "grok", "", p("Notification", `,"notificationType":"idle_prompt"`))
	wantState(t, "grok", "k1", "done", "")
	// A subagent's turn says nothing about the parent session.
	hook(t, "grok", "", p("UserPromptSubmit", `,"subagentType":"explore"`))
	wantState(t, "grok", "k1", "done", "")
	hook(t, "grok", "", p("UserPromptSubmit", ""))
	hook(t, "grok", "", p("StopCancelled", `,"reason":"interrupted"`))
	wantState(t, "grok", "k1", "done", "StopCancelled")
	hook(t, "grok", "", p("StopFailure", ""))
	wantState(t, "grok", "k1", "stopped", "StopFailure")
	hook(t, "grok", "", p("SessionEnd", ""))
	wantNoRecord(t, "grok", "k1")
}

func TestClaudeHookInsideGrokIsRecordedAsGrok(t *testing.T) {
	withTempHome(t)
	t.Setenv("GROK_SESSION_ID", "k2")
	hook(t, "claude", "", `{"hook_event_name":"UserPromptSubmit","sessionId":"k2","cwd":"/work"}`)
	wantState(t, "grok", "k2", "working", "UserPromptSubmit")
	wantNoRecord(t, "claude", "k2")
}

func TestHookPiLifecycle(t *testing.T) {
	withTempHome(t)
	p := func(event, extra string) string {
		return `{"event":"` + event + `","session_id":"pi1","cwd":"/work"` + extra + `}`
	}
	hook(t, "pi", "", p("agent_start", ""))
	wantState(t, "pi", "pi1", "working", "agent_start")
	hook(t, "pi", "", p("ui_prompt_start", ""))
	wantState(t, "pi", "pi1", "waiting", "ui_prompt_start")
	hook(t, "pi", "", p("ui_prompt_end", ""))
	wantState(t, "pi", "pi1", "working", "ui_prompt_end")
	hook(t, "pi", "", p("agent_settled", `,"aborted":true`))
	wantState(t, "pi", "pi1", "done", "aborted")
	hook(t, "pi", "", p("agent_settled", `,"aborted":false`))
	wantState(t, "pi", "pi1", "done", "agent-turn-complete")
	// reload/new/resume/fork keep the process; only quit ends the session.
	hook(t, "pi", "", p("session_shutdown", `,"reason":"reload"`))
	wantState(t, "pi", "pi1", "done", "")
	hook(t, "pi", "", p("session_shutdown", `,"reason":"quit"`))
	wantNoRecord(t, "pi", "pi1")
}

func TestHookOmpLifecycle(t *testing.T) {
	withTempHome(t)
	p := func(event, extra string) string {
		return `{"event":"` + event + `","session_id":"o1","cwd":"/work"` + extra + `}`
	}
	hook(t, "omp", "", p("agent_start", ""))
	wantState(t, "omp", "o1", "working", "agent_start")
	hook(t, "omp", "", p("tool_execution_start", `,"tool_name":"ask"`))
	wantState(t, "omp", "o1", "waiting", "ask")
	hook(t, "omp", "", p("tool_execution_end", `,"tool_name":"ask"`))
	wantState(t, "omp", "o1", "working", "tool_execution_end")
	hook(t, "omp", "", p("tool_approval_requested", `,"tool_name":"bash"`))
	wantState(t, "omp", "o1", "waiting", "tool_approval_requested")
	hook(t, "omp", "", p("tool_approval_resolved", ""))
	wantState(t, "omp", "o1", "working", "tool_approval_resolved")
	// An automatic continuation follows: not a settle.
	hook(t, "omp", "", p("agent_end", `,"will_continue":true`))
	wantState(t, "omp", "o1", "working", "")
	hook(t, "omp", "", p("agent_end", `,"will_continue":false`))
	wantState(t, "omp", "o1", "done", "agent-turn-complete")
	hook(t, "omp", "", p("session_shutdown", ""))
	wantNoRecord(t, "omp", "o1")
}

// --- setup ---

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupOK(t *testing.T, args ...string) string {
	t.Helper()
	rc, stdout, stderr := runCLI(t, append([]string{"setup"}, args...)...)
	if rc != exitOK {
		t.Fatalf("setup %v: exit %d: %s", args, rc, stderr)
	}
	return stdout
}

func TestSetupGeminiMergesAndIsIdempotent(t *testing.T) {
	home := withTempHome(t)
	settings := filepath.Join(home, ".gemini", "settings.json")
	writeFile(t, settings, `{"theme":"dark","hooks":{"AfterAgent":[{"hooks":[{"type":"command","command":"notify-me"}]}]}}`)
	setupOK(t, "gemini")
	setupOK(t, "gemini")
	got := readJSON(t, settings)
	if got["theme"] != "dark" {
		t.Fatalf("other settings lost: %v", got)
	}
	hooks := got["hooks"].(map[string]any)
	after := hooks["AfterAgent"].([]any)
	if len(after) != 2 {
		t.Fatalf("AfterAgent: want the user's hook plus ai-attn's, got %v", after)
	}
	for _, h := range geminiHookEvents {
		if n := strings.Count(mustJSON(t, hooks[h.event]), geminiHookMarker); n != 1 {
			t.Fatalf("%s: want one ai-attn hook, got %d", h.event, n)
		}
	}
	if !strings.Contains(mustJSON(t, hooks["Notification"]), `"matcher":"ToolPermission"`) {
		t.Fatalf("Notification matcher missing: %v", hooks["Notification"])
	}
}

func TestSetupGeminiRefusesCommentedSettings(t *testing.T) {
	home := withTempHome(t)
	writeFile(t, filepath.Join(home, ".gemini", "settings.json"), "{\n  // mine\n  \"theme\": \"dark\"\n}\n")
	if rc, _, _ := runCLI(t, "setup", "gemini"); rc == exitOK {
		t.Fatal("setup gemini should refuse a commented settings.json without --force")
	}
	setupOK(t, "gemini", "--force")
}

func TestSetupAgyKeepsOtherGroups(t *testing.T) {
	home := withTempHome(t)
	path := filepath.Join(home, ".gemini", "config", "hooks.json")
	writeFile(t, path, `{"mine":{"enabled":true,"Stop":[{"type":"command","command":"echo"}]}}`)
	setupOK(t, "agy")
	setupOK(t, "agy")
	got := readJSON(t, path)
	if _, ok := got["mine"]; !ok {
		t.Fatalf("user group lost: %v", got)
	}
	group := got[agyHookGroup].(map[string]any)
	for _, event := range agyHookEvents {
		if !strings.Contains(mustJSON(t, group[event]), "agy.sh "+event) {
			t.Fatalf("%s: hook missing or without its event argument: %v", event, group[event])
		}
	}
}

func TestSetupCopilotAndGrokWriteOwnFiles(t *testing.T) {
	home := withTempHome(t)
	setupOK(t, "copilot")
	copilot := readJSON(t, filepath.Join(home, ".copilot", "hooks", "ai-attn.json"))
	if copilot["version"].(float64) != 1 {
		t.Fatalf("copilot hooks need version 1: %v", copilot)
	}
	if s := mustJSON(t, copilot["hooks"]); !strings.Contains(s, "copilot.sh agentStop") ||
		!strings.Contains(s, `"matcher":"permission_prompt|elicitation_dialog"`) {
		t.Fatalf("copilot hooks: %s", s)
	}

	t.Setenv("GROK_HOME", filepath.Join(home, "grok-home"))
	setupOK(t, "grok")
	grok := readJSON(t, filepath.Join(home, "grok-home", "hooks", "ai-attn.json"))
	if s := mustJSON(t, grok["hooks"]); !strings.Contains(s, "StopCancelled") ||
		!strings.Contains(s, `"matcher":"permission_prompt|elicitation_dialog"`) {
		t.Fatalf("grok hooks: %s", s)
	}
}

func TestSetupPiCopiesExtensionButNotOverForeignFile(t *testing.T) {
	home := withTempHome(t)
	if rc, _, _ := runCLI(t, "setup", "pi"); rc == exitOK {
		t.Fatal("setup pi should fail when the extension is not installed")
	}
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "plugins", "pi", "ai-attn.ts"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".local", "share", "ai-attn", "plugins", "pi", "ai-attn.ts"), string(src))
	setupOK(t, "pi")
	target := filepath.Join(home, ".pi", "agent", "extensions", "ai-attn.ts")
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, src) {
		t.Fatal("extension not copied")
	}
	setupOK(t, "pi") // refreshing our own file is fine
	writeFile(t, target, "export default function () {}\n")
	if rc, _, _ := runCLI(t, "setup", "pi"); rc == exitOK {
		t.Fatal("setup pi must not replace a file ai-attn did not write")
	}
}

func TestSetupDetectsNewAgents(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("PATH", t.TempDir()) // no agent binaries
	for _, dir := range []string{".copilot", ".grok"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out := setupOK(t, "--dry-run")
	for _, want := range []string{"copilot: would install", "grok: would install"} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"gemini:", "agy:", "pi:", "omp:"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("did not expect %q in:\n%s", unwanted, out)
		}
	}
}

func TestDoctorReportsNewAgents(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("PATH", t.TempDir())
	writeFile(t, filepath.Join(home, ".local", "share", "ai-attn", "hooks", "copilot.sh"), "#!/bin/sh\n")
	if err := os.MkdirAll(filepath.Join(home, ".copilot"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runCLI(t, "doctor")
	if !strings.Contains(out, "hook_copilot=not_wired") || !strings.Contains(out, "hook_grok=not_installed") {
		t.Fatalf("doctor before setup:\n%s", out)
	}
	setupOK(t, "copilot")
	_, out, _ = runCLI(t, "doctor")
	if !strings.Contains(out, "hook_copilot=installed") {
		t.Fatalf("doctor after setup:\n%s", out)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
