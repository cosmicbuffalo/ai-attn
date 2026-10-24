package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// doctorHome sets up a temp HOME with all hook scripts installed and Claude
// wired, so each test only has to describe the agent it is about.
func doctorHome(t *testing.T) string {
	t.Helper()
	home := withTempHome(t)
	hookDir := filepath.Join(home, ".local", "share", "ai-attn", "hooks")
	mustMkdir(t, hookDir)
	for _, hook := range []string{"claude.sh", "codex.sh", "opencode.sh"} {
		mustWrite(t, filepath.Join(hookDir, hook), "#!/usr/bin/env bash\n")
	}
	mustMkdir(t, filepath.Join(home, ".claude"))
	mustWrite(t, filepath.Join(home, ".claude", "settings.json"),
		`{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"bash ~/.local/share/ai-attn/hooks/claude.sh"}]}]}}`)
	return home
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func expectDoctor(t *testing.T, wantRC int, want ...string) string {
	t.Helper()
	rc, stdout, _ := runCLI(t, "doctor")
	if rc != wantRC {
		t.Fatalf("doctor rc=%d, want %d; output:\n%s", rc, wantRC, stdout)
	}
	for _, field := range want {
		if !strings.Contains(stdout, field) {
			t.Fatalf("expected %q in doctor output:\n%s", field, stdout)
		}
	}
	return stdout
}

// codexHooksJSON wires ai-attn for two events, PermissionRequest behind an
// unrelated hook so its index within the group is 1.
const codexHooksJSON = `{"hooks":{
  "PermissionRequest":[{"matcher":"","hooks":[
    {"type":"command","command":"notify-me"},
    {"type":"command","command":"bash '/x/.local/share/ai-attn/hooks/codex.sh'"}]}],
  "PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"bash '/x/.local/share/ai-attn/hooks/codex.sh'"}]}]
}}`

func writeCodex(t *testing.T, home, configTOML string) string {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	mustMkdir(t, dir)
	hooksPath := filepath.Join(dir, "hooks.json")
	mustWrite(t, hooksPath, codexHooksJSON)
	if configTOML != "" {
		mustWrite(t, filepath.Join(dir, "config.toml"), strings.ReplaceAll(configTOML, "HOOKS", hooksPath))
	}
	return hooksPath
}

func TestDoctorFlagsUntrustedCodexHooks(t *testing.T) {
	home := doctorHome(t)
	writeCodex(t, home, "")
	codexVersion = func() string { return "codex-cli 0.163.0" }

	expectDoctor(t, exitError,
		"hook_codex=untrusted (Codex has not approved 2 of 2 ai-attn hooks",
		"permission_request:0:1", "pre_tool_use:0:0", "/hooks", "Some checks failed.")
}

func TestDoctorAcceptsTrustedCodexHooks(t *testing.T) {
	home := doctorHome(t)
	writeCodex(t, home, `
[hooks.state."HOOKS:permission_request:0:1"]
trusted_hash = "sha256:aa"

[hooks.state."HOOKS:pre_tool_use:0:0"]
trusted_hash = "sha256:bb"
`)
	codexVersion = func() string { return "codex-cli 0.163.0" }

	expectDoctor(t, exitOK, "hook_codex=installed (trusted in Codex; hashes not verified)", "All checks passed.")
}

// A config that already records approvals means Codex enforces trust even
// when its version cannot be read.
func TestDoctorFlagsPartlyTrustedCodexHooksWithoutVersion(t *testing.T) {
	home := doctorHome(t)
	writeCodex(t, home, `
[hooks.state."HOOKS:pre_tool_use:0:0"]
trusted_hash = "sha256:bb"
`)

	out := expectDoctor(t, exitError, "hook_codex=untrusted (Codex has not approved 1 of 2", "permission_request:0:1")
	if strings.Contains(out, "pre_tool_use:0:0") {
		t.Fatalf("trusted hook reported as untrusted:\n%s", out)
	}
}

func TestDoctorSkipsCodexTrustOnOlderCodex(t *testing.T) {
	home := doctorHome(t)
	writeCodex(t, home, "")
	codexVersion = func() string { return "codex-cli 0.162.1" }

	expectDoctor(t, exitOK, "hook_codex=installed\n")
}

func TestDoctorHonorsCodexHome(t *testing.T) {
	home := doctorHome(t)
	alt := filepath.Join(home, "alt-codex")
	mustMkdir(t, alt)
	mustWrite(t, filepath.Join(alt, "hooks.json"), codexHooksJSON)
	t.Setenv("CODEX_HOME", alt)
	codexVersion = func() string { return "codex-cli 0.163.0" }

	expectDoctor(t, exitError, "hook_codex=untrusted", filepath.Join(alt, "hooks.json"))
}

func TestDoctorFlagsCommentedOutOpencodePlugin(t *testing.T) {
	home := doctorHome(t)
	dir := filepath.Join(home, ".config", "opencode")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, "opencode.jsonc"), `{
  // "plugin": [
  //   "/x/.local/share/ai-attn/plugins/opencode"
  // ],
  "theme": "dark"
}`)

	expectDoctor(t, exitError, "hook_opencode=not_wired (the ai-attn plugin entry", "commented out")
}

func TestDoctorAcceptsOpencodePluginInJSONC(t *testing.T) {
	home := doctorHome(t)
	dir := filepath.Join(home, ".config", "opencode")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, "opencode.jsonc"), `{
  // ai-attn signals
  "plugin": [
    "/x/.local/share/ai-attn/plugins/opencode", /* trailing */
  ],
}`)

	expectDoctor(t, exitOK, "hook_opencode=installed")
}

func TestDoctorReadsOpencodeJSON(t *testing.T) {
	home := doctorHome(t)
	dir := filepath.Join(home, ".config", "opencode")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, "opencode.json"), `{"plugin":["/x/.local/share/ai-attn/plugins/opencode"]}`)

	expectDoctor(t, exitOK, "hook_opencode=installed")
}

func TestDoctorFlagsClaudeConfigDirWithoutHooks(t *testing.T) {
	home := doctorHome(t)
	alt := filepath.Join(home, "alt-claude")
	mustMkdir(t, alt)
	mustWrite(t, filepath.Join(alt, "settings.json"), `{"model":"opus"}`)
	t.Setenv("CLAUDE_CONFIG_DIR", alt)

	expectDoctor(t, exitError, "hook_claude_config_dir=not_wired (CLAUDE_CONFIG_DIR="+alt, "send no signals")
}

func TestDoctorAcceptsClaudeConfigDirWithHooks(t *testing.T) {
	home := doctorHome(t)
	alt := filepath.Join(home, "alt-claude")
	mustMkdir(t, alt)
	mustWrite(t, filepath.Join(alt, "settings.json"),
		`{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"bash ~/.local/share/ai-attn/hooks/claude.sh"}]}]}}`)
	t.Setenv("CLAUDE_CONFIG_DIR", alt)

	expectDoctor(t, exitOK, "hook_claude_config_dir=installed")
}

func TestDoctorIgnoresClaudeConfigDirPointingAtDefault(t *testing.T) {
	home := doctorHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")+"/")

	out := expectDoctor(t, exitOK)
	if strings.Contains(out, "hook_claude_config_dir") {
		t.Fatalf("default config dir reported separately:\n%s", out)
	}
}

func museReturns(body string, err error) {
	museInspect = func() ([]byte, bool, error) { return []byte(body), true, err }
}

func TestDoctorMuseNotInstalled(t *testing.T) {
	doctorHome(t)
	expectDoctor(t, exitOK, "hook_muse=not_installed")
}

func TestDoctorMuseTrustedHooks(t *testing.T) {
	doctorHome(t)
	museReturns(`{"active":true,"record":{"enabled":true},"runtime_capabilities":[
		{"candidate":{"kind":"hook","capability_id":"stop"},"status":"trusted_enabled"},
		{"candidate":{"kind":"hook","capability_id":"permission-request"},"status":"trusted_enabled"}]}`, nil)

	expectDoctor(t, exitOK, "hook_muse=installed")
}

func TestDoctorMuseHooksNeedApproval(t *testing.T) {
	doctorHome(t)
	museReturns(`{"active":true,"record":{"enabled":true},"runtime_capabilities":[
		{"candidate":{"kind":"hook","capability_id":"stop"},"status":"trusted_enabled"},
		{"candidate":{"kind":"hook","capability_id":"permission-request"},"status":"pending"}]}`, nil)

	expectDoctor(t, exitError, "hook_muse=needs_approval (Muse has not enabled 1 of 2", "permission-request=pending", "muse plugins approve ai-attn")
}

func TestDoctorMusePluginMissing(t *testing.T) {
	doctorHome(t)
	museReturns(`{"error":{"code":"unknown-plugin","message":"plugin `+"`ai-attn`"+` is not installed"}}`, errors.New("exit status 1"))

	expectDoctor(t, exitError, "hook_muse=not_wired", "ai-attn setup muse")
}

func TestDoctorMusePluginDisabled(t *testing.T) {
	doctorHome(t)
	museReturns(`{"active":false,"record":{"enabled":false},"runtime_capabilities":[]}`, nil)

	expectDoctor(t, exitError, "hook_muse=disabled", "muse plugins enable ai-attn")
}

func TestDoctorMuseUnreadableOutput(t *testing.T) {
	doctorHome(t)
	museReturns("plugins are unavailable in this build", errors.New("exit status 2"))

	expectDoctor(t, exitError, "hook_muse=config_error")
}

func TestSnakeCase(t *testing.T) {
	for in, want := range map[string]string{
		"PreToolUse":        "pre_tool_use",
		"PermissionRequest": "permission_request",
		"Stop":              "stop",
		"UserPromptSubmit":  "user_prompt_submit",
	} {
		if got := snakeCase(in); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	for out, want := range map[string]bool{
		"codex-cli 0.163.0": true,
		"codex-cli 0.163.2": true,
		"codex-cli 1.0.0":   true,
		"codex-cli 0.162.9": false,
		"":                  false,
	} {
		if got := versionAtLeast(out, codexMinTrustVersion); got != want {
			t.Errorf("versionAtLeast(%q) = %t, want %t", out, got, want)
		}
	}
}

// With no approvals on record, only Codex's version says whether it
// enforces trust; when that is unknown doctor says the check did not run.
func TestDoctorSaysCodexTrustUncheckedWithoutVersion(t *testing.T) {
	home := doctorHome(t)
	writeCodex(t, home, "")

	expectDoctor(t, exitOK, "hook_codex=installed (codex not found in PATH or did not answer, so hook trust was not checked)")
}

func writeOpencodeWrapper(t *testing.T, home, config string) {
	t.Helper()
	mustWrite(t, filepath.Join(home, "wrap.js"), `export { default } from "`+home+`/.local/share/ai-attn/plugins/opencode/index.mjs";`)
	dir := filepath.Join(home, ".config", "opencode")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, "opencode.jsonc"), config)
}

// An OpenCode plugin entry that wraps ai-attn's plugin counts as wired when
// the wrapper is in the "plugin" list, written either way.
func TestDoctorAcceptsOpencodeWrapperPlugin(t *testing.T) {
	for _, entry := range []string{"~/wrap.js", "HOME/wrap.js"} {
		home := doctorHome(t)
		writeOpencodeWrapper(t, home, `{"plugin": ["`+strings.Replace(entry, "HOME", home, 1)+`"]}`)

		expectDoctor(t, exitOK, "hook_opencode=installed (via wrapper at "+filepath.Join(home, "wrap.js")+")")
	}
}

func TestDoctorFlagsCommentedOutOpencodeWrapperPlugin(t *testing.T) {
	home := doctorHome(t)
	writeOpencodeWrapper(t, home, "{\n  // \"plugin\": [\"~/wrap.js\"],\n  \"plugin\": []\n}")

	expectDoctor(t, exitError, "hook_opencode=not_wired")
}

// writeProbe puts an executable script named bin first on PATH.
func writeProbe(t *testing.T, bin, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A probe whose launcher leaves a child holding stdout must still give up at
// the timeout, and say that it timed out.
func TestProbeTimeoutStopsWaitingForGrandchildren(t *testing.T) {
	prevTimeout, prevDelay := probeTimeout, probeWaitDelay
	probeTimeout, probeWaitDelay = 200*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { probeTimeout, probeWaitDelay = prevTimeout, prevDelay })
	writeProbe(t, "muse", "sleep 3 &\nsleep 3\n")

	start := time.Now()
	_, found, err := defaultMuseInspect()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("probe took %s", elapsed)
	}
	if !found || err == nil || !strings.Contains(err.Error(), "did not answer within") {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

// Doctor only reads, so its muse probe must not let the launcher update Muse.
func TestMuseProbeDisablesLauncherAutoUpdate(t *testing.T) {
	writeProbe(t, "muse", `printf '%s %s' "$MUSE_NO_AUTO_UPDATE" "$MUSE_EXPERIMENTAL_PLUGINS"`)

	out, found, err := defaultMuseInspect()
	if !found || err != nil || string(out) != "1 1" {
		t.Fatalf("found=%v err=%v out=%q", found, err, out)
	}
}
