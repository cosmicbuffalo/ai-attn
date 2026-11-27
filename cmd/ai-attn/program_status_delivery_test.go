package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHookRetriesUnsentProgramStatus(t *testing.T) {
	for _, failure := range []string{"discovery", "open", "write"} {
		t.Run(failure, func(t *testing.T) {
			writeConfig(t, withTempHome(t), "osc7501 = true\n")
			tty := withFakeTTY(t)
			reportPath := tty
			lookups := 0
			terminalPath = func() string {
				lookups++
				return reportPath
			}
			const stop = `{"hook_event_name":"Stop","session_id":"delivery-retry"}`
			if got := hookReport(t, tty, "cortex", stop); !strings.Contains(got, "state=done:") {
				t.Fatalf("initial stop report = %q", got)
			}
			switch failure {
			case "discovery":
				reportPath = ""
			case "open":
				reportPath = filepath.Join(t.TempDir(), "missing-terminal")
			case "write":
				if _, err := os.Stat("/dev/full"); err != nil {
					t.Skipf("write-error fixture unavailable: %v", err)
				}
				reportPath = "/dev/full"
			}
			var stdout, stderr bytes.Buffer
			const prompt = `{"hook_event_name":"UserPromptSubmit","session_id":"delivery-retry"}`
			if rc := run([]string{"hook", "--agent", "cortex"}, strings.NewReader(prompt), &stdout, &stderr); rc != exitOK {
				t.Fatalf("prompt = %d, stderr %q", rc, stderr.String())
			}
			if failure == "write" && !strings.Contains(stderr.String(), "failed to write program status") {
				t.Fatalf("expected a terminal write failure, got %q", stderr.String())
			}
			if got, err := os.ReadFile(tty); err != nil || !strings.Contains(string(got), "state=done:") {
				t.Fatalf("failed report changed the terminal: %q, %v", got, err)
			}
			key := sessionKey("cortex", "delivery-retry", "", "")
			if record, err := readRecord(stateFile(key)); err != nil || record.State != "working" {
				t.Fatalf("failed report must still save working: %+v, %v", record, err)
			}
			reportPath = tty
			const tool = `{"hook_event_name":"PreToolUse","session_id":"delivery-retry"}`
			const want = "\x1b]7501;state=working:msg=V29ya2luZw==:app=cortex\x1b\\"
			if got := hookReport(t, tty, "cortex", tool); got != want {
				t.Errorf("recovered terminal report = %q, want %q", got, want)
			}
			if lookups != 3 {
				t.Errorf("lookups after failed attempt and recovery = %d, want 3", lookups)
			}
			beforeHeartbeat := lookups
			if got := hookReport(t, tty, "cortex", tool); got != "" || lookups != beforeHeartbeat {
				t.Errorf("heartbeat after recovery wrote %q, lookups = %d, want %d", got, lookups, beforeHeartbeat)
			}
		})
	}
}

func TestHookBacksOffRepeatedProgramStatusFailures(t *testing.T) {
	writeConfig(t, withTempHome(t), "osc7501 = true\n")
	tty := withFakeTTY(t)
	lookups := 0
	reportPath := ""
	terminalPath = func() string {
		lookups++
		return reportPath
	}
	const prompt = `{"hook_event_name":"UserPromptSubmit","session_id":"delivery-backoff"}`
	const tool = `{"hook_event_name":"PreToolUse","session_id":"delivery-backoff"}`
	hookReport(t, tty, "cortex", prompt)
	hookReport(t, tty, "cortex", tool)
	if lookups != 2 {
		t.Fatalf("lookups = %d, want initial attempt and immediate retry", lookups)
	}
	reportPath = tty
	if got := hookReport(t, tty, "cortex", tool); got != "" || lookups != 2 {
		t.Fatalf("retry during backoff wrote %q, lookups = %d", got, lookups)
	}
	path := stateFile(sessionKey("cortex", "delivery-backoff", "", "")) + programStatusReceiptSuffix
	receipt := readProgramStatusReceipt(path)
	receipt.RetryAt = time.Now().Add(-time.Second).UnixMilli()
	if err := writeProgramStatusReceipt(path, receipt); err != nil {
		t.Fatal(err)
	}
	if got := hookReport(t, tty, "cortex", tool); !strings.Contains(got, "state=working:") || lookups != 3 {
		t.Fatalf("retry after backoff wrote %q, lookups = %d", got, lookups)
	}

	// A new report body is eligible during backoff on the previous one.
	reportPath = ""
	const waiting = `{"hook_event_name":"PermissionRequest","session_id":"delivery-backoff"}`
	hookReport(t, tty, "cortex", waiting)
	hookReport(t, tty, "cortex", waiting)
	reportPath = tty
	const stop = `{"hook_event_name":"Stop","session_id":"delivery-backoff"}`
	if got := hookReport(t, tty, "cortex", stop); !strings.Contains(got, "state=done:") || lookups != 6 {
		t.Fatalf("new state during backoff wrote %q, lookups = %d", got, lookups)
	}
}

func TestProgramStatusReceiptCleanup(t *testing.T) {
	for _, action := range []string{"session-end", "clear", "gc"} {
		t.Run(action, func(t *testing.T) {
			writeConfig(t, withTempHome(t), "osc7501 = true\n")
			tty := withFakeTTY(t)
			hookReport(t, tty, "cortex", `{"hook_event_name":"Stop","session_id":"cleanup"}`)
			path := stateFile(sessionKey("cortex", "cleanup", "", ""))
			receiptPath := path + programStatusReceiptSuffix
			if _, err := os.Stat(receiptPath); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "session-end":
				hookReport(t, tty, "cortex", `{"hook_event_name":"SessionEnd","session_id":"cleanup"}`)
			case "clear":
				if rc := cmdClear(nil, &bytes.Buffer{}, &bytes.Buffer{}); rc != exitOK {
					t.Fatalf("clear returned %d", rc)
				}
			case "gc":
				record, err := readRecord(path)
				if err != nil {
					t.Fatal(err)
				}
				record.UpdatedAt = 1
				if err := writeJSON(path, record); err != nil {
					t.Fatal(err)
				}
				if removed, err := runGC(defaultConfig()); removed != 1 || err != nil {
					t.Fatalf("gc removed %d records, err = %v", removed, err)
				}
			}
			if _, err := os.Stat(receiptPath); !os.IsNotExist(err) {
				t.Fatalf("receipt after %s: %v, want file absent", action, err)
			}
		})
	}
}
