package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// registerIdentityFlags registers the session-identity flags (agent, cwd,
// session-id, pane-id) on the given FlagSet.
func registerIdentityFlags(fs *flag.FlagSet) *sessionIdentity {
	cwd, _ := os.Getwd()
	flags := &sessionIdentity{}
	fs.StringVar(&flags.Agent, "agent", "", "Agent name (claude, codex)")
	fs.StringVar(&flags.CWD, "cwd", cwd, "Working directory")
	fs.StringVar(&flags.SessionID, "session-id", "", "Session identifier")
	fs.StringVar(&flags.PaneID, "pane-id", os.Getenv("TMUX_PANE"), "Tmux pane ID")
	return flags
}

// cmdSetState implements the set-state and clear-state subcommands, toggling the session's attention state.
// `waiting=true` is the set-state path, which requires an explicit --state flag (working/waiting/done/stopped);
// `waiting=false` is the clear-state path, which writes an empty state regardless of --state.
func cmdSetState(args []string, stdout, stderr io.Writer, waiting bool) int {
	fs := flag.NewFlagSet("set-state", flag.ContinueOnError)
	fs.SetOutput(stderr)
	identityFlags := registerIdentityFlags(fs)
	var reason string
	fs.StringVar(&reason, "reason", "", "Reason for the state change")
	state := ""
	fs.StringVar(&state, "state", "", "State value (working, waiting, done, stopped)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if identityFlags.Agent == "" {
		fmt.Fprintln(stderr, "error: --agent is required")
		return exitUsage
	}
	if waiting && state == "" {
		fmt.Fprintln(stderr, "error: --state is required (working, waiting, done, stopped)")
		return exitUsage
	}
	if !waiting {
		state = ""
	}

	cfg := loadConfig(stderr)
	if state == "waiting" && !cfg.Enabled {
		return exitOK
	}
	if err := ensureStateDir(); err != nil {
		fmt.Fprintln(stderr, "ai-attn: failed to create state dir:", err)
		return exitError
	}

	identity := sessionIdentity{
		Agent:     identityFlags.Agent,
		CWD:       identityFlags.CWD,
		SessionID: identityFlags.SessionID,
		PaneID:    identityFlags.PaneID,
	}
	record, err := writeStateRecord(identity, state, reason)
	if err != nil {
		fmt.Fprintf(stderr, "ai-attn: failed to write state record: %v\n", err)
		return exitError
	}
	fmt.Fprintln(stdout, record.SessionKey)
	return exitOK
}

// cmdStatus implements the status subcommand, printing the current state of a session and exiting 1 if waiting.
func cmdStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	identityFlags := registerIdentityFlags(fs)
	key := fs.String("key", "", "Session key (overrides other identity flags)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	cfg := loadConfig(stderr)
	runGC(cfg)
	if *key == "" {
		*key = sessionKey(identityFlags.Agent, identityFlags.SessionID, identityFlags.PaneID)
	}
	data, err := os.ReadFile(stateFile(*key))
	if err != nil {
		fmt.Fprintf(stdout, "state= key=%s reason=none age=na\n", *key)
		return exitOK
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		fmt.Fprintf(stdout, "state= key=%s reason=invalid age=na\n", *key)
		return exitOK
	}
	normalizeRecord(&record, time.Now().Unix())
	fmt.Fprintf(stdout, "state=%s key=%s reason=%s age=%ds\n", record.State, *key, record.Reason, record.AgeSeconds)
	if record.State == "waiting" {
		return exitError
	}
	return exitOK
}

// normalizeRecord computes and sets the AgeSeconds field on a Record relative to the given timestamp.
func normalizeRecord(rec *Record, nowUnix int64) {
	age := nowUnix - rec.UpdatedAt
	if age < 0 {
		age = 0
	}
	rec.AgeSeconds = age
}

// runGC removes stale state files that have exceeded the configured TTL. Called by status, list, and the gc subcommand.
func runGC(cfg Config) (int, error) {
	dir := stateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	now := time.Now().Unix()
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if removeErr := os.Remove(path); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				removed++
			}
			continue
		}
		var record Record
		if err := json.Unmarshal(data, &record); err != nil {
			if removeErr := os.Remove(path); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				removed++
			}
			continue
		}
		age := now - record.UpdatedAt
		if age > int64(cfg.TTLSeconds) {
			if err := os.Remove(path); err == nil || errors.Is(err, os.ErrNotExist) {
				removed++
			}
		}
	}
	return removed, nil
}

// cmdGC implements the gc subcommand, running garbage collection and printing the count of removed files.
func cmdGC(stdout, stderr io.Writer) int {
	cfg := loadConfig(stderr)
	removed, err := runGC(cfg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	fmt.Fprintf(stdout, "removed=%d\n", removed)
	return exitOK
}

// listRecords reads all state files, normalizes their ages, and returns them sorted by agent/cwd/session.
func listRecords(now int64) ([]Record, error) {
	dir := stateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Record{}, nil
		}
		return nil, err
	}
	records := []Record{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var record Record
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}
		normalizeRecord(&record, now)
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.Agent != b.Agent {
			return a.Agent < b.Agent
		}
		if a.CWD != b.CWD {
			return a.CWD < b.CWD
		}
		if a.SessionID != b.SessionID {
			return a.SessionID < b.SessionID
		}
		return a.SessionKey < b.SessionKey
	})
	return records, nil
}

// cmdList implements the list subcommand, displaying all tracked sessions in table or JSON format.
func cmdList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "Output as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	cfg := loadConfig(stderr)
	runGC(cfg)
	now := time.Now().Unix()
	records, err := listRecords(now)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}

	if *jsonOutput {
		payload := listPayload{
			GeneratedAt: now,
			TTLSeconds:  cfg.TTLSeconds,
			Records:     records,
		}
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(payload); err != nil {
			fmt.Fprintln(stderr, err)
			return exitError
		}
		return exitOK
	}

	if len(records) == 0 {
		fmt.Fprintln(stdout, "No sessions tracked.")
		return exitOK
	}

	green := "\033[32m"
	yellow := "\033[33m"
	red := "\033[31m"
	dim := "\033[2m"
	bold := "\033[1m"
	reset := "\033[0m"

	if f, ok := stdout.(*os.File); !ok || !isTerminal(f) {
		green, yellow, red, dim, bold, reset = "", "", "", "", "", ""
	}

	fmt.Fprintf(stdout, "%s%-8s  %-10s  %-8s  %-24s  %s%s\n", bold, "AGENT", "STATE", "AGE", "REASON", "CWD", reset)
	for _, rec := range records {
		if rec.State == "" {
			continue
		}
		var stateColorCode string
		switch rec.State {
		case "waiting":
			stateColorCode = yellow
		case "working":
			stateColorCode = dim
		case "done":
			stateColorCode = green
		case "stopped":
			stateColorCode = red
		default:
			stateColorCode = dim
		}

		age := formatAge(rec.AgeSeconds)
		cwd := rec.CWD
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, home) {
			cwd = "~" + cwd[len(home):]
		}
		reason := rec.Reason
		if reason == "" {
			reason = "-"
		}
		fmt.Fprintf(stdout, "%-8s  %s%-10s%s  %-8s  %-24s  %s\n",
			rec.Agent,
			stateColorCode, padRight(rec.State, 10), reset,
			age,
			reason,
			cwd,
		)
	}
	return exitOK
}

// cmdClear implements the clear subcommand, removing waiting/done state files (optionally scoped to the current pane).
func cmdClear(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("clear", flag.ContinueOnError)
	fs.SetOutput(stderr)
	paneOnly := fs.Bool("pane", false, "Only clear the current pane (uses $TMUX_PANE)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	dir := stateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stdout, "cleared=0")
			return exitOK
		}
		fmt.Fprintln(stderr, err)
		return exitError
	}

	currentPane := ""
	if *paneOnly {
		currentPane = os.Getenv("TMUX_PANE")
		if currentPane == "" {
			fmt.Fprintln(stderr, "clear --pane: $TMUX_PANE is not set")
			return exitUsage
		}
	}

	cleared := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		record, err := readRecord(path)
		if err != nil || (record.State != "waiting" && record.State != "done") {
			continue
		}
		if *paneOnly && record.PaneID != currentPane {
			continue
		}
		if err := os.Remove(path); err != nil {
			fmt.Fprintf(stderr, "warning: failed to clear %s: %s\n", entry.Name(), err)
			continue
		}
		cleared++
	}
	fmt.Fprintf(stdout, "cleared=%d\n", cleared)
	return exitOK
}
