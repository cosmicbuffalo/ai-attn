package main

import "testing"

// TestSessionKeyStableAcrossPaneChanges verifies that the session key ignores pane ID when a session ID is present.
func TestSessionKeyStableAcrossPaneChanges(t *testing.T) {
	key1 := sessionKey("claude", "session-uuid-123", "%1")
	key2 := sessionKey("claude", "session-uuid-123", "%99")
	if key1 != key2 {
		t.Fatalf("session key with session ID should ignore pane: %s vs %s", key1, key2)
	}
}

// TestSessionKeySameWithoutSessionID verifies that keys are stable when derived from pane ID alone.
func TestSessionKeySameWithoutSessionID(t *testing.T) {
	key1 := sessionKey("codex", "", "%1")
	key2 := sessionKey("codex", "", "%1")
	if key1 != key2 {
		t.Fatalf("keys without session_id should match on pane: %s vs %s", key1, key2)
	}
}

// TestSessionKeyDiffersBetweenAgents verifies that different agents with the same session ID produce different keys.
func TestSessionKeyDiffersBetweenAgents(t *testing.T) {
	key1 := sessionKey("claude", "same-session", "")
	key2 := sessionKey("codex", "same-session", "")
	if key1 == key2 {
		t.Fatal("keys for different agents with same session_id should differ")
	}
}
