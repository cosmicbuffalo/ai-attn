package main

import (
	"strings"
	"testing"
)

// TestHelpExitsZero verifies that --help exits with code 0 and prints usage text.
func TestHelpExitsZero(t *testing.T) {
	rc, stdout, _ := runCLI(t, "--help")
	if rc != exitOK {
		t.Fatalf("--help rc=%d", rc)
	}
	if !strings.Contains(stdout, "set-state") {
		t.Fatalf("expected usage in help output: %s", stdout)
	}
}

// TestVersionExitsZero verifies that --version exits with code 0 and prints the version string.
func TestVersionExitsZero(t *testing.T) {
	rc, stdout, _ := runCLI(t, "--version")
	if rc != exitOK {
		t.Fatalf("--version rc=%d", rc)
	}
	if !strings.Contains(stdout, "ai-attn") {
		t.Fatalf("expected version output: %s", stdout)
	}
}

// TestNoArgsShowsHelp verifies that running with no arguments prints usage and exits 0.
func TestNoArgsShowsHelp(t *testing.T) {
	rc, stdout, _ := runCLI(t)
	if rc != exitOK {
		t.Fatalf("no args rc=%d", rc)
	}
	if !strings.Contains(stdout, "usage:") {
		t.Fatalf("expected usage output: %s", stdout)
	}
}

// TestUnknownCommandExitsTwo verifies that an unrecognized subcommand exits with code 2.
func TestUnknownCommandExitsTwo(t *testing.T) {
	rc, _, stderr := runCLI(t, "bogus")
	if rc != exitUsage {
		t.Fatalf("expected rc=2, got %d", rc)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("expected unknown command error: %s", stderr)
	}
}

// TestSubcommandHelpExitsZero verifies that every subcommand accepts -h and exits 0.
func TestSubcommandHelpExitsZero(t *testing.T) {
	commands := []string{
		"list", "clear", "logs", "status", "doctor",
		"gc", "init-config", "set-state", "clear-state", "hook", "setup",
	}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			rc, _, _ := runCLI(t, cmd, "-h")
			if rc != exitOK {
				t.Fatalf("%s -h: expected exit 0, got %d", cmd, rc)
			}
		})
	}
}

// TestSetStateHelpIncludesExpectedFlags verifies that set-state -h includes identity and reason flags.
func TestSetStateHelpIncludesExpectedFlags(t *testing.T) {
	_, _, stderr := runCLI(t, "set-state", "-h")
	for _, flag := range []string{"agent", "pane-id", "reason"} {
		if !strings.Contains(stderr, flag) {
			t.Fatalf("set-state -h should show flag %q: %s", flag, stderr)
		}
	}
}
