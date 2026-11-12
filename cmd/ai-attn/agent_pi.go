package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// --- Pi and Oh My Pi ---
//
// Both load TypeScript extensions from a directory, so setup copies
// ai-attn's extension there. The extension runs in-process and pipes each
// event to `ai-attn hook --agent pi|omp` as JSON:
//
//	{"event": "...", "session_id": "...", "cwd": "...", ...event fields}
//
// The mapping from events to states lives here, not in the extension, so it
// is tested with the rest of ai-attn. The extensions serialize their writes,
// since neither agent awaits every handler.

const extensionMarker = "managed by ai-attn"

type extensionAgent struct {
	name   string
	source string // file in ~/.local/share/ai-attn/plugins
	dir    func() string
}

var piAgent = extensionAgent{
	name:   "pi",
	source: filepath.Join("pi", "ai-attn.ts"),
	dir: func() string {
		return filepath.Join(envDir("PI_CODING_AGENT_DIR", filepath.Join(homeDir(), ".pi", "agent")), "extensions")
	},
}

var ompAgent = extensionAgent{
	name:   "omp",
	source: filepath.Join("omp", "ai-attn.ts"),
	dir: func() string {
		return filepath.Join(homeDir(), ".omp", "agent", "extensions")
	},
}

func (a extensionAgent) target() string {
	return filepath.Join(a.dir(), "ai-attn.ts")
}

func (a extensionAgent) installed() bool {
	return dirExists(filepath.Dir(a.dir()))
}

func hookExtension(agent, hookPayload string, stderr io.Writer) int {
	var payload map[string]any
	if err := json.Unmarshal([]byte(hookPayload), &payload); err != nil {
		return exitOK
	}
	event := stringFromMap(payload, "event")
	sessionID := stringFromMap(payload, "session_id")
	if sessionID == "" {
		sessionID = fallbackSessionID(agent)
	}
	cwd := cwdOrWorkdir(stringFromMap(payload, "cwd"))

	var state, reason string
	var ok bool
	if agent == "pi" {
		state, reason, ok = matchPiEvent(event, payload)
	} else {
		state, reason, ok = matchOmpEvent(event, payload)
	}
	if !ok {
		return exitOK
	}
	if state == "" {
		removeSessionRecord(agent, sessionID, stderr)
		return exitOK
	}
	return writeHookState(state, agent, sessionID, cwd, reason, stderr)
}

// matchPiEvent maps a Pi extension event to a state. An empty state with ok
// means the session ended and its record should go.
//
// agent_end is not the end of a turn (Pi may retry, compact or continue);
// agent_settled is. Pi has no permission prompts of its own, so waiting
// covers only extension dialogs (ui_prompt_*, Pi 0.84.4 and later).
func matchPiEvent(event string, payload map[string]any) (state, reason string, ok bool) {
	switch event {
	case "agent_start", "ui_prompt_end":
		return "working", event, true
	case "ui_prompt_start":
		return "waiting", event, true
	case "agent_settled":
		if aborted, _ := payload["aborted"].(bool); aborted {
			return "done", "aborted", true
		}
		return "done", "agent-turn-complete", true
	case "session_shutdown":
		// reload, new, resume and fork are followed by a new session_start
		// in the same process; only quit ends the session.
		if stringFromMap(payload, "reason") == "quit" {
			return "", "", true
		}
	}
	return "", "", false
}

// matchOmpEvent maps an Oh My Pi extension event to a state, like
// matchPiEvent. agent_end with willContinue is not a settle: an automatic
// continuation or background work follows. Waiting covers tool approvals
// (rare, since the default approval mode is yolo) and the ask tool.
func matchOmpEvent(event string, payload map[string]any) (state, reason string, ok bool) {
	switch event {
	case "agent_start", "tool_approval_resolved":
		return "working", event, true
	case "tool_approval_requested":
		return "waiting", event, true
	case "tool_execution_start":
		if stringFromMap(payload, "tool_name") == "ask" {
			return "waiting", "ask", true
		}
		return "working", event, true
	case "tool_execution_end":
		if stringFromMap(payload, "tool_name") == "ask" {
			return "working", event, true
		}
	case "agent_end":
		if cont, _ := payload["will_continue"].(bool); cont {
			return "", "", false
		}
		return "done", "agent-turn-complete", true
	case "session_shutdown":
		return "", "", true
	}
	return "", "", false
}

func setupExtension(a extensionAgent, stdout, stderr io.Writer, dryRun bool) int {
	source := filepath.Join(homeDir(), ".local", "share", "ai-attn", "plugins", a.source)
	data, err := os.ReadFile(source)
	if err != nil {
		fmt.Fprintf(stderr, "%s: extension not found at %s\n", a.name, source)
		fmt.Fprintln(stderr, "  Re-run the installer, or 'make install' from a source checkout.")
		return exitError
	}
	target := a.target()
	if existing, err := os.ReadFile(target); err == nil && !containsMarker(existing) {
		fmt.Fprintf(stderr, "%s: refusing to replace %s: it was not written by ai-attn\n", a.name, target)
		return exitError
	}
	return writeSetupFile(stdout, stderr, a.name, target, data, "extension", dryRun)
}

func containsMarker(data []byte) bool {
	return strings.Contains(string(data), extensionMarker)
}
