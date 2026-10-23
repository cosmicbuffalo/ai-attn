package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
)

// Doctor's agent probes run external commands. Tests replace these so they
// never depend on what is installed on the machine running them.
var (
	// codexVersion returns `codex --version` output, or "" when Codex is not
	// on PATH or does not answer.
	codexVersion = defaultCodexVersion
	// museInspect returns `muse plugins inspect ai-attn --json` stdout.
	// found is false when muse is not on PATH.
	museInspect = defaultMuseInspect
)

const probeTimeout = 10 * time.Second

func defaultCodexVersion() string {
	bin, err := exec.LookPath("codex")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func defaultMuseInspect() (out []byte, found bool, err error) {
	bin, lookErr := exec.LookPath("muse")
	if lookErr != nil {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "plugins", "inspect", musePluginID, "--json")
	cmd.Env = append(os.Environ(), "MUSE_EXPERIMENTAL_PLUGINS=1")
	out, err = cmd.Output()
	return out, true, err
}

// codexHome is Codex's config directory: $CODEX_HOME, else ~/.codex.
func codexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return expandHome(dir)
	}
	return filepath.Join(homeDir(), ".codex")
}

// opencodeConfigFile is the OpenCode config doctor inspects: opencode.jsonc,
// or opencode.json when only that exists.
func opencodeConfigFile() string {
	dir := filepath.Join(homeDir(), ".config", "opencode")
	jsonc := filepath.Join(dir, "opencode.jsonc")
	if _, err := os.Stat(jsonc); err != nil {
		if _, err := os.Stat(filepath.Join(dir, "opencode.json")); err == nil {
			return filepath.Join(dir, "opencode.json")
		}
	}
	return jsonc
}

// reportWiredHook prints the status of an agent whose config references
// ai-attn, after the agent-specific checks that a plain reference does not
// prove: OpenCode entries that are commented out and Codex hooks Codex has
// not trusted. via is the wrapper script the config calls, if any. It returns
// false when the hook will not actually fire.
func reportWiredHook(stdout io.Writer, agent, configFile string, configData []byte, searchStr, via string) bool {
	suffix := ""
	if via != "" {
		suffix = fmt.Sprintf(" (via wrapper at %s)", via)
	}
	switch agent {
	case "opencode":
		if !opencodePluginEnabled(configData, searchStr) {
			fmt.Fprintf(stdout, "hook_%s=not_wired (the ai-attn plugin entry in %s is commented out or outside the \"plugin\" list, so OpenCode never loads it) — add it to \"plugin\" (see AGENTS.md)\n", agent, configFile)
			return false
		}
	case "codex":
		missing, total, required, err := codexUntrustedHooks(configFile, configData, via)
		if err != nil {
			fmt.Fprintf(stdout, "hook_%s=config_error (could not check Codex hook trust: %s)\n", agent, err)
			return false
		}
		if required && len(missing) > 0 {
			fmt.Fprintf(stdout, "hook_%s=untrusted (Codex has not approved %d of %d ai-attn hooks in %s: %s; Codex skips untrusted hooks, so it sends no signals) — open Codex and approve the hooks (/hooks)\n",
				agent, len(missing), total, configFile, strings.Join(missing, ", "))
			return false
		}
		if required && total > 0 {
			if via != "" {
				suffix = fmt.Sprintf(" (via wrapper at %s; trusted in Codex, hashes not verified)", via)
			} else {
				suffix = " (trusted in Codex; hashes not verified)"
			}
		}
	}
	fmt.Fprintf(stdout, "hook_%s=installed%s\n", agent, suffix)
	return true
}

// opencodePluginEnabled reports whether searchStr appears as an entry of the
// config's "plugin" list once comments are removed. A config that cannot be
// parsed falls back to a plain search of the uncommented text.
func opencodePluginEnabled(configData []byte, searchStr string) bool {
	stripped := stripJSONCComments(string(configData))
	if !strings.Contains(stripped, searchStr) {
		return false
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(stripTrailingCommas(stripped)), &config); err != nil {
		return true
	}
	plugins, _ := config["plugin"].([]any)
	for _, p := range plugins {
		if s, ok := p.(string); ok && strings.Contains(s, searchStr) {
			return true
		}
	}
	return false
}

// codexMinTrustVersion is the first Codex release that only runs hooks the
// user has approved, recording each approval in config.toml.
var codexMinTrustVersion = [3]int{0, 163, 0}

var semverRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func versionAtLeast(output string, min [3]int) bool {
	m := semverRe.FindStringSubmatch(output)
	if m == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		n, _ := strconv.Atoi(m[i+1])
		if n != min[i] {
			return n > min[i]
		}
	}
	return true
}

// codexUntrustedHooks lists ai-attn's hooks in hooks.json that have no trust
// entry in config.toml. Codex keys each approval as
// "<hooks.json path>:<event_snake_case>:<group index>:<hook index>" under
// [hooks.state]. The trusted_hash itself is Codex's own digest and is not
// recomputed here, so a hook edited after approval is not detected.
//
// Trust is required when Codex is new enough to enforce it, or when
// config.toml already records approvals (Codex version unknown).
func codexUntrustedHooks(hooksPath string, hooksData []byte, via string) (missing []string, total int, required bool, err error) {
	var settings map[string]any
	if err := json.Unmarshal(hooksData, &settings); err != nil {
		return nil, 0, false, fmt.Errorf("parse %s: %w", hooksPath, err)
	}
	isAiAttn := func(command string) bool {
		if codexCommandIsAiAttn(command) {
			return true
		}
		if via == "" {
			return false
		}
		if strings.Contains(command, via) {
			return true
		}
		if rel, ok := strings.CutPrefix(via, homeDir()+string(filepath.Separator)); ok {
			return strings.Contains(command, "~/"+rel)
		}
		return false
	}

	var keys []string
	events, _ := settings["hooks"].(map[string]any)
	eventNames := make([]string, 0, len(events))
	for event := range events {
		eventNames = append(eventNames, event)
	}
	sort.Strings(eventNames)
	for _, event := range eventNames {
		groups, _ := events[event].([]any)
		for i, g := range groups {
			group, _ := g.(map[string]any)
			hooks, _ := group["hooks"].([]any)
			for j, h := range hooks {
				hook, _ := h.(map[string]any)
				command, _ := hook["command"].(string)
				if isAiAttn(command) {
					keys = append(keys, fmt.Sprintf("%s:%s:%d:%d", hooksPath, snakeCase(event), i, j))
				}
			}
		}
	}

	state := map[string]any{}
	configPath := filepath.Join(filepath.Dir(hooksPath), "config.toml")
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		var config map[string]any
		if _, err := toml.Decode(string(data), &config); err != nil {
			return nil, 0, false, fmt.Errorf("parse %s: %w", configPath, err)
		}
		if hooksTable, ok := config["hooks"].(map[string]any); ok {
			if s, ok := hooksTable["state"].(map[string]any); ok {
				state = s
			}
		}
	}

	required = len(state) > 0 || versionAtLeast(codexVersion(), codexMinTrustVersion)
	for _, key := range keys {
		entry, _ := state[key].(map[string]any)
		hash, _ := entry["trusted_hash"].(string)
		enabled, hasEnabled := entry["enabled"].(bool)
		name := key[len(hooksPath)+1:]
		switch {
		case hash == "":
			missing = append(missing, name)
		case hasEnabled && !enabled:
			missing = append(missing, name+" (disabled)")
		}
	}
	return missing, len(keys), required, nil
}

// snakeCase turns a Codex hook event name (PreToolUse) into the form Codex
// uses in trust keys (pre_tool_use).
func snakeCase(s string) string {
	var out strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				out.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		out.WriteRune(r)
	}
	return out.String()
}

// checkClaudeConfigDir checks the Claude config directory named by
// CLAUDE_CONFIG_DIR, when it is not ~/.claude. Claude reads hooks only from
// that directory's settings.json, so sessions using it send no signals unless
// the ai-attn hooks are wired there too.
func checkClaudeConfigDir(stdout io.Writer) bool {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		return true
	}
	dir = filepath.Clean(expandHome(dir))
	if dir == filepath.Join(homeDir(), ".claude") {
		return true
	}
	settings := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(settings)
	if err == nil {
		if strings.Contains(string(data), claudeHookSuffix) {
			fmt.Fprintf(stdout, "hook_claude_config_dir=installed (CLAUDE_CONFIG_DIR=%s)\n", dir)
			return true
		}
		if wrapper := findWrapperReferencingCanonical(data, claudeHookSuffix); wrapper != "" {
			fmt.Fprintf(stdout, "hook_claude_config_dir=installed (CLAUDE_CONFIG_DIR=%s, via wrapper at %s)\n", dir, wrapper)
			return true
		}
	}
	fmt.Fprintf(stdout, "hook_claude_config_dir=not_wired (CLAUDE_CONFIG_DIR=%s, but %s has no ai-attn hooks; Claude sessions using that config dir send no signals) — unset CLAUDE_CONFIG_DIR, or copy the ai-attn hooks from ~/.claude/settings.json into it\n", dir, settings)
	return false
}

// museInspectResult is the subset of `muse plugins inspect --json` doctor reads.
type museInspectResult struct {
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Active bool `json:"active"`
	Record struct {
		Enabled bool `json:"enabled"`
	} `json:"record"`
	RuntimeCapabilities []struct {
		Candidate struct {
			Kind         string `json:"kind"`
			CapabilityID string `json:"capability_id"`
		} `json:"candidate"`
		Status string `json:"status"`
	} `json:"runtime_capabilities"`
}

// checkMuse reports whether Muse has the ai-attn plugin installed, enabled,
// and its hooks trusted. Muse runs a plugin's hooks only after
// `muse plugins approve`, and needs a fresh approval for every new bundle.
func checkMuse(stdout io.Writer) bool {
	out, found, runErr := museInspect()
	if !found {
		fmt.Fprintln(stdout, "hook_muse=not_installed (muse not found in PATH; not checked)")
		return true
	}
	var result museInspectResult
	if err := json.Unmarshal(out, &result); err != nil {
		reason := strings.TrimSpace(string(out))
		if runErr != nil {
			reason = runErr.Error()
		}
		fmt.Fprintf(stdout, "hook_muse=config_error (could not inspect the Muse plugin: %s)\n", reason)
		return false
	}
	if result.Error != nil {
		if result.Error.Code == "unknown-plugin" {
			fmt.Fprintln(stdout, "hook_muse=not_wired (the ai-attn plugin is not installed in Muse) — run 'ai-attn setup muse' to fix")
		} else {
			fmt.Fprintf(stdout, "hook_muse=config_error (muse: %s)\n", result.Error.Message)
		}
		return false
	}
	if !result.Record.Enabled || !result.Active {
		fmt.Fprintf(stdout, "hook_muse=disabled (the ai-attn plugin is installed but not enabled) — run 'muse plugins enable %s'\n", musePluginID)
		return false
	}
	var hooks, inert []string
	for _, c := range result.RuntimeCapabilities {
		if c.Candidate.Kind != "hook" {
			continue
		}
		hooks = append(hooks, c.Candidate.CapabilityID)
		if c.Status != "trusted_enabled" {
			inert = append(inert, c.Candidate.CapabilityID+"="+c.Status)
		}
	}
	switch {
	case len(hooks) == 0:
		fmt.Fprintf(stdout, "hook_muse=needs_approval (Muse lists no runnable ai-attn hooks) — run 'muse plugins approve %s'\n", musePluginID)
		return false
	case len(inert) > 0:
		fmt.Fprintf(stdout, "hook_muse=needs_approval (Muse has not enabled %d of %d ai-attn hooks: %s) — run 'muse plugins approve %s'\n",
			len(inert), len(hooks), strings.Join(inert, ", "), musePluginID)
		return false
	}
	fmt.Fprintln(stdout, "hook_muse=installed")
	return true
}
