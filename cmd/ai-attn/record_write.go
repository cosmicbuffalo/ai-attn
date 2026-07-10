package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sessionKey derives a deterministic, truncated SHA-256 key from agent,
// session identity, and the tmux server that owns the pane. Empty socket keeps
// the pre-v0.3.2 key format for records created outside tmux and migrations.
func sessionKey(agent, sessionID, paneID, socket string) string {
	agent = strings.TrimSpace(agent)
	sessionID = strings.TrimSpace(sessionID)
	socket = strings.TrimSpace(socket)

	// All supported agents provide or synthesise a session ID, so the
	// fallback path (no session ID) is essentially dead code — but we
	// still handle it using pane ID to disambiguate.
	var hashInput string
	if sessionID != "" {
		hashInput = agent + "|" + sessionID
	} else {
		hashInput = agent + "|" + strings.TrimSpace(paneID)
	}
	if socket != "" {
		hashInput += "|" + socket
	}
	sum := sha256.Sum256([]byte(hashInput))
	return hex.EncodeToString(sum[:])[:20]
}

func legacySessionKey(agent, sessionID, paneID string) string {
	return sessionKey(agent, sessionID, paneID, "")
}

// stateFile returns the absolute path to the JSON state file for the given session key.
func stateFile(key string) string {
	return filepath.Join(stateDir(), key+".json")
}

// tmuxSocket returns the tmux server socket path from $TMUX (its first
// comma-separated field: "<socket>,<pid>,<session>"), or "" when not running
// inside tmux. Recorded on each state record so consumers can scope a pane ID
// to the server that owns it — pane IDs are not unique across tmux servers.
func tmuxSocket() string {
	v := os.Getenv("TMUX")
	if v == "" {
		return ""
	}
	if i := strings.IndexByte(v, ','); i >= 0 {
		return v[:i]
	}
	return v
}

// writeJSON marshals a value to JSON and writes it to the given path.
func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// readRecord reads and unmarshals a Record from the JSON file at path.
func readRecord(path string) (Record, error) {
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(fileBytes, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// writeStateRecord writes the session's state file.
//
// Concurrency note: multiple hook invocations for the same session can race
// here (e.g., PreToolUse fires just before Notification). This is intentional
// — last-write-wins is the correct behaviour. The downstream consumer
// (tmux-ai-attn) polls state periodically and only cares about the most
// recent value, so momentary flickers from concurrent writes are harmless.
func writeStateRecord(identity sessionIdentity, state, reason string) (Record, error) {
	key := sessionKey(identity.Agent, identity.SessionID, identity.PaneID, identity.Socket)

	record := Record{
		Agent:      identity.Agent,
		SessionKey: key,
		State:      state,
		Reason:     reason,
		UpdatedAt:  time.Now().Unix(),
		CWD:        identity.CWD,
		SessionID:  identity.SessionID,
		PaneID:     identity.PaneID,
		Socket:     identity.Socket,
	}
	if err := writeJSON(stateFile(key), record); err != nil {
		return Record{}, err
	}
	if err := removeCompatibleLegacyRecord(identity, key); err != nil {
		return Record{}, err
	}
	return record, nil
}

func removeCompatibleLegacyRecord(identity sessionIdentity, currentKey string) error {
	legacyKey := legacySessionKey(identity.Agent, identity.SessionID, identity.PaneID)
	if legacyKey == currentKey {
		return nil
	}
	path := stateFile(legacyKey)
	legacy, err := readRecord(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return os.Remove(path)
	}
	if legacy.Agent != strings.TrimSpace(identity.Agent) ||
		legacy.SessionID != strings.TrimSpace(identity.SessionID) {
		return nil
	}
	if legacy.Socket != "" && legacy.Socket != strings.TrimSpace(identity.Socket) {
		return nil
	}
	return os.Remove(path)
}
