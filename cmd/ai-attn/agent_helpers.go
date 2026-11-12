package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Helpers shared by the agents whose hooks were added after the original
// five: Gemini CLI, Antigravity, Copilot CLI, Grok Build, Pi and Oh My Pi.

// hookScriptCommand is the command an agent config runs for one of
// ai-attn's hook scripts, with optional arguments.
func hookScriptCommand(script string, args ...string) string {
	cmd := "bash ~/.local/share/ai-attn/hooks/" + script
	if len(args) > 0 {
		cmd += " " + strings.Join(args, " ")
	}
	return cmd
}

// fallbackSessionID keys a record to the tmux pane, or failing that the
// hook's parent process, when an agent's payload names no session.
func fallbackSessionID(agent string) string {
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		return agent + "-pane-" + strings.TrimPrefix(pane, "%")
	}
	return fmt.Sprintf("%s-ppid-%d", agent, os.Getppid())
}

// cwdOrWorkdir returns cwd, or the hook's working directory when it is empty.
func cwdOrWorkdir(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return cwd
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func onPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// envDir returns the directory an environment variable names (with a
// leading ~ expanded), or fallback when it is unset.
func envDir(name, fallback string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	if v == "~" {
		return homeDir()
	}
	if strings.HasPrefix(v, "~/") {
		return filepath.Join(homeDir(), v[2:])
	}
	return v
}

// readJSONObject reads a JSON object from path; a missing file is an empty
// object. With allowComments, // and /* */ comments are accepted, but since
// writing the file back drops them, a commented file is refused unless force.
func readJSONObject(agent, path string, allowComments, force bool) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %v", path, err)
	}
	text := string(data)
	if allowComments && hasJSONCComments(text) {
		if !force {
			return nil, fmt.Errorf("%s: refusing to overwrite %s: it contains comments that setup would drop; "+
				"remove them, wire the hooks by hand (see AGENTS.md), or re-run with --force", agent, path)
		}
		text = stripJSONCComments(text)
	}
	if strings.TrimSpace(text) == "" {
		return map[string]any{}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %v", path, err)
	}
	if obj == nil {
		obj = map[string]any{}
	}
	return obj, nil
}

// writeSetupFile writes data to path for agent's setup, or reports what it
// would do under --dry-run. what describes the change for the message.
func writeSetupFile(stdout, stderr io.Writer, agent, path string, data []byte, what string, dryRun bool) int {
	if dryRun {
		fmt.Fprintf(stdout, "%s: would install %s in %s (dry-run)\n", agent, what, path)
		return exitOK
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(stderr, "failed to create directory: %v\n", err)
		return exitError
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(stderr, "failed to write %s: %v\n", path, err)
		return exitError
	}
	fmt.Fprintf(stdout, "%s: installed %s in %s\n", agent, what, path)
	return exitOK
}

func marshalJSONFile(v any) ([]byte, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// claudeShapedHookGroup is one `{"matcher": ..., "hooks": [command]}` group,
// the shape Claude Code, Gemini CLI and Grok Build share. timeout is in the
// agent's own unit.
func claudeShapedHookGroup(command, matcher string, timeout int) map[string]any {
	return map[string]any{
		"matcher": matcher,
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": command,
			"timeout": timeout,
		}},
	}
}

// replaceHookGroups drops every hook whose command contains marker from a
// Claude-shaped hooks object, then appends groups. Other tools' hooks stay.
func replaceHookGroups(hooks map[string]any, marker string, groups map[string][]any) {
	for event, val := range hooks {
		kept := removeHookEntries(val, func(command string) bool {
			return strings.Contains(command, marker)
		})
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	for event, add := range groups {
		existing, _ := hooks[event].([]any)
		hooks[event] = append(existing, add...)
	}
}

// checkMoreAgentHooks reports, for doctor, whether each of the newer agents
// that is installed has ai-attn wired in. It returns false when an installed
// agent would send no signals.
func checkMoreAgentHooks(stdout io.Writer) bool {
	hookDir := filepath.Join(homeDir(), ".local", "share", "ai-attn", "hooks")
	checks := []struct {
		agent     string
		installed bool
		script    string // hook script that must exist, or "" for extensions
		file      string
		marker    string
	}{
		{"gemini", geminiInstalled(), "gemini.sh", filepath.Join(geminiHome(), "settings.json"), geminiHookMarker},
		{"agy", agyInstalled(), "agy.sh", agyHooksPath(), agyHookMarker},
		{"copilot", dirExists(copilotHome()), "copilot.sh", copilotHooksFile(), copilotHookMarker},
		{"grok", dirExists(grokHome()), "grok.sh", grokHooksFile(), grokHookMarker},
		{"pi", piAgent.installed(), "", piAgent.target(), extensionMarker},
		{"omp", ompAgent.installed(), "", ompAgent.target(), extensionMarker},
	}
	ok := true
	for _, c := range checks {
		if !c.installed {
			fmt.Fprintf(stdout, "hook_%s=not_installed (agent not found; not checked)\n", c.agent)
			continue
		}
		if c.script != "" && !fileExists(filepath.Join(hookDir, c.script)) {
			fmt.Fprintf(stdout, "hook_%s=missing (script not found at %s) — re-run the ai-attn installer\n",
				c.agent, filepath.Join(hookDir, c.script))
			ok = false
			continue
		}
		data, err := os.ReadFile(c.file)
		if err != nil || !strings.Contains(string(data), c.marker) {
			fmt.Fprintf(stdout, "hook_%s=not_wired (no ai-attn hooks in %s) — run 'ai-attn setup %s' to fix\n",
				c.agent, c.file, c.agent)
			ok = false
			continue
		}
		fmt.Fprintf(stdout, "hook_%s=installed\n", c.agent)
	}
	return ok
}
