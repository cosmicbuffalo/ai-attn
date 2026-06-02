package main

import (
	"os"
	"testing"
)

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

// TestTmuxSocketParsing verifies that the socket path is extracted from the
// first comma-separated field of $TMUX, and is empty outside tmux.
func TestTmuxSocketParsing(t *testing.T) {
	cases := []struct {
		name string
		tmux string
		want string
	}{
		{"unset", "", ""},
		{"standard", "/tmp/tmux-1000/default,12345,0", "/tmp/tmux-1000/default"},
		{"named socket", "/home/nick/.tmux-sockets/homelab,999,2", "/home/nick/.tmux-sockets/homelab"},
		{"inner tmux", "/tmp/tmux-1000/inner,4845,1780271390-jwu66opc", "/tmp/tmux-1000/inner"},
		{"no commas", "/tmp/tmux-1000/odd", "/tmp/tmux-1000/odd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMUX", tc.tmux)
			if tc.tmux == "" {
				os.Unsetenv("TMUX")
			}
			if got := tmuxSocket(); got != tc.want {
				t.Fatalf("tmuxSocket() = %q, want %q", got, tc.want)
			}
		})
	}
}
