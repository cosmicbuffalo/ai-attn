package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// cmdLogs implements the logs subcommand, tailing the hook event log via the system tail command.
func cmdLogs(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lines := fs.Int("n", 20, "Number of lines to show")
	follow := fs.Bool("f", false, "Follow the log (like tail -f)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	logPath := filepath.Join(stateDir(), "hook.log")
	if _, err := os.Stat(logPath); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "No log file at %s\n", logPath)
		return exitError
	}

	tailArgs := []string{"-n", fmt.Sprintf("%d", *lines)}
	if *follow {
		tailArgs = append(tailArgs, "-f")
	}
	tailArgs = append(tailArgs, logPath)

	cmd := exec.Command("tail", tailArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "failed to start tail: %v\n", err)
		return exitError
	}
	// Forward SIGINT/SIGTERM to the child so it shuts down cleanly
	// (especially important for `tail -f`).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		if sig, ok := <-sigCh; ok {
			_ = cmd.Process.Signal(sig)
		}
	}()
	err := cmd.Wait()
	signal.Stop(sigCh)
	close(sigCh)
	if err != nil {
		return exitError
	}
	return exitOK
}

// cmdTest implements the test subcommand, firing a temporary waiting signal then clearing it after a delay.
func cmdTest(stdout, stderr io.Writer) int {
	paneID := os.Getenv("TMUX_PANE")
	cwd, _ := os.Getwd()
	sessionID := "ai-attn-test"
	agent := "test"

	cfg := loadConfig(stderr)
	if !cfg.Enabled {
		fmt.Fprintln(stderr, "ai-attn is disabled in config")
		return exitError
	}
	if err := ensureStateDir(); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}

	key := sessionKey(agent, sessionID, paneID)

	// Handle interrupt so we clean up the test state file on Ctrl-C.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()
	interrupted := func() bool {
		select {
		case <-sigCh:
			fmt.Fprintln(stdout, "\nInterrupted — clearing test signal...")
			os.Remove(stateFile(key))
			return true
		default:
			return false
		}
	}

	fmt.Fprintln(stdout, "Setting test signal: permission (3s)...")
	record := Record{
		Agent:      agent,
		SessionKey: key,
		State:      "waiting",
		Reason:     "permission_request",
		UpdatedAt:  time.Now().Unix(),
		CWD:        cwd,
		SessionID:  sessionID,
		PaneID:     paneID,
	}
	if err := writeJSON(stateFile(key), record); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	time.Sleep(3 * time.Second)
	if interrupted() {
		return exitOK
	}

	fmt.Fprintln(stdout, "Setting test signal: done (3s)...")
	record.State = "done"
	record.Reason = "agent-turn-complete"
	record.UpdatedAt = time.Now().Unix()
	if err := writeJSON(stateFile(key), record); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	time.Sleep(3 * time.Second)
	if interrupted() {
		return exitOK
	}

	fmt.Fprintln(stdout, "Clearing test signal...")
	os.Remove(stateFile(key))
	fmt.Fprintln(stdout, "Done. If your status bar updated correctly, ai-attn is working.")
	return exitOK
}

// cmdDoctor implements the doctor subcommand, checking config, state dir, and hook installation health.
func cmdDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	allPassed := true
	fmt.Fprintf(stdout, "ai-attn %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(stdout, "config=%s\n", configPath())
	fmt.Fprintf(stdout, "state_dir=%s\n", stateDir())

	cfg := defaultConfig()
	cfgData, cfgErr := os.ReadFile(configPath())
	if cfgErr != nil {
		if errors.Is(cfgErr, os.ErrNotExist) {
			// Missing config is the default-install state, not an error.
			fmt.Fprintln(stdout, "config_status=default (no config file; using built-in defaults)")
		} else {
			fmt.Fprintf(stdout, "config_status=error (%s)\n", cfgErr)
			allPassed = false
		}
	} else {
		parsed, err := parseConfig(cfgData)
		if err != nil {
			fmt.Fprintf(stdout, "config_status=invalid_config (%s)\n", err)
			allPassed = false
		} else {
			cfg = parsed
			fmt.Fprintln(stdout, "config_status=ok")
		}
	}

	fmt.Fprintf(stdout, "enabled=%t\n", cfg.Enabled)
	fmt.Fprintf(stdout, "ttl_seconds=%d\n", cfg.TTLSeconds)

	if err := os.MkdirAll(stateDir(), 0o755); err != nil {
		fmt.Fprintf(stdout, "state_dir_status=error (%s)\n", err)
		allPassed = false
	} else {
		probe := filepath.Join(stateDir(), ".doctor-probe")
		if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
			fmt.Fprintf(stdout, "state_dir_status=not_writable (%s)\n", err)
			allPassed = false
		} else {
			_ = os.Remove(probe)
			fmt.Fprintln(stdout, "state_dir_status=ok")
		}
	}

	hookDir := filepath.Join(homeDir(), ".local", "share", "ai-attn", "hooks")
	type hookCheck struct {
		script     string
		configFile string
		searchStr  string
	}
	hooks := []hookCheck{
		{"claude.sh", filepath.Join(homeDir(), ".claude", "settings.json"), "ai-attn/hooks/claude.sh"},
		{"codex.sh", filepath.Join(homeDir(), ".codex", "config.toml"), "ai-attn/hooks/codex.sh"},
		{"opencode.sh", filepath.Join(homeDir(), ".config", "opencode", "opencode.jsonc"), "ai-attn/plugins/opencode"},
	}
	for _, check := range hooks {
		agent := strings.TrimSuffix(check.script, ".sh")
		scriptPath := filepath.Join(hookDir, check.script)
		if _, err := os.Stat(scriptPath); err != nil {
			fmt.Fprintf(stdout, "hook_%s=missing (script not found at %s)\n", agent, scriptPath)
			allPassed = false
			continue
		}
		configData, err := os.ReadFile(check.configFile)
		if err != nil {
			fmt.Fprintf(stdout, "hook_%s=not_wired (script exists but not referenced in %s) — run 'ai-attn setup' to fix\n", agent, check.configFile)
			allPassed = false
			continue
		}
		if strings.Contains(string(configData), check.searchStr) {
			fmt.Fprintf(stdout, "hook_%s=installed\n", agent)
			continue
		}
		if wrapper := findWrapperReferencingCanonical(configData, check.searchStr); wrapper != "" {
			fmt.Fprintf(stdout, "hook_%s=installed (via wrapper at %s)\n", agent, wrapper)
			continue
		}
		fmt.Fprintf(stdout, "hook_%s=not_wired (script exists but not referenced in %s) — run 'ai-attn setup' to fix\n", agent, check.configFile)
		allPassed = false
	}

	if allPassed {
		fmt.Fprintln(stdout, "\nAll checks passed.")
		return exitOK
	}
	fmt.Fprintln(stdout, "\nSome checks failed. See above for details.")
	return exitError
}

// findWrapperReferencingCanonical scans the agent's config for paths to script files,
// reads each candidate script, and returns the first one whose contents reference the
// canonical hook path (searchStr). Returns "" if no such wrapper is found.
//
// This lets doctor recognize setups like `notify = ["bash", "/path/to/codex-multi.sh"]`
// where codex-multi.sh is a user-authored fan-out that ultimately invokes our canonical
// codex.sh — strictly the canonical path isn't in the config, but the wiring still
// reaches our hook.
func findWrapperReferencingCanonical(configData []byte, searchStr string) string {
	for _, candidate := range candidatePathsInConfig(configData) {
		expanded := expandHome(candidate)
		info, err := os.Stat(expanded)
		if err != nil || info.IsDir() {
			continue
		}
		body, err := os.ReadFile(expanded)
		if err != nil {
			continue
		}
		if strings.Contains(string(body), searchStr) {
			return expanded
		}
	}
	return ""
}

// candidatePathsInConfig returns paths mentioned in the agent's config that
// might be wrapper scripts pointing at ai-attn. The extraction is intentionally
// permissive — anything that looks like an absolute path or a ~/ path is fair
// game, and the caller will filter by whether the file exists and contains the
// canonical hook reference.
// pathRe matches absolute or ~/-rooted paths in a config file, stopping at
// characters that can't appear in shell/JSON/TOML path literals (quotes,
// whitespace, common structural punctuation).
var pathRe = regexp.MustCompile(`~?/[^\s"',\[\]{}():;<>]+`)

func candidatePathsInConfig(configData []byte) []string {
	var paths []string
	seen := map[string]bool{}
	for _, match := range pathRe.FindAllString(string(configData), -1) {
		if seen[match] {
			continue
		}
		seen[match] = true
		paths = append(paths, match)
	}
	return paths
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}

// cmdInitConfig implements the init-config subcommand, creating a default config file if one does not exist.
// init-config is optional: ai-attn runs fine without a config file (defaults apply). It exists for users who
// want to override the defaults or just want a self-documenting starting point.
func cmdInitConfig(stdout, stderr io.Writer) int {
	if err := ensureAllDirs(); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	path := configPath()
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(stdout, "exists=%s\n", path)
		return exitOK
	}
	if err := os.WriteFile(path, []byte(defaultConfigTOML), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	fmt.Fprintf(stdout, "created=%s\n", path)
	return exitOK
}

// defaultConfigTOML is what `init-config` writes when no config file exists.
// Values match defaultConfig(); comments document each key for users who land here from the docs.
const defaultConfigTOML = `# ai-attn configuration. All keys are optional — defaults apply when omitted.

# Master switch. When false, set-state writes are no-ops (the binary stays installed
# and hooks still fire, they just don't record state).
enabled = true

# How long a record stays valid before it is treated as expired and garbage-collected.
ttl_seconds = 259200
`
