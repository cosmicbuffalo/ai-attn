package main

import (
	"fmt"
	"io"
	"os"
)

// main is the program entry point; it delegates to run with OS-level I/O.
func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run dispatches CLI arguments to the appropriate subcommand and returns an exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return exitOK
	}

	switch args[0] {
	case "set-state":
		return cmdSetState(args[1:], stdout, stderr, true)
	case "clear-state":
		return cmdSetState(args[1:], stdout, stderr, false)
	case "status":
		return cmdStatus(args[1:], stdout, stderr)
	case "gc":
		return cmdGC(stdout, stderr)
	case "list":
		return cmdList(args[1:], stdout, stderr)
	case "doctor":
		return cmdDoctor(args[1:], stdout, stderr)
	case "init-config":
		return cmdInitConfig(stdout, stderr)
	case "hook":
		return cmdHook(args[1:], stdin, stdout, stderr)
	case "clear":
		return cmdClear(args[1:], stdout, stderr)
	case "logs":
		return cmdLogs(args[1:], stdout, stderr)
	case "test":
		return cmdTest(stdout, stderr)
	case "setup":
		return cmdSetup(args[1:], stdout, stderr)
	case "--help", "-h", "help":
		printUsage(stdout)
		return exitOK
	case "--version", "-v", "version":
		fmt.Fprintf(stdout, "ai-attn %s\n", version)
		return exitOK
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr)
		return exitUsage
	}
}

// printUsage writes the top-level help text to w. Called for --help, -h, or no arguments.
func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `usage: ai-attn <command> [flags]

Commands:
  list           List all tracked sessions
  clear          Clear all waiting signals (or current pane with --pane)
  logs           Tail the hook event log
  test           Fire a test signal and clear it after a few seconds
  status         Check the waiting status of a session
  doctor         Check installation health
  gc             Remove stale state files
  init-config    Create default config file
  set-state      Record an agent state (working/waiting/done/stopped)
  clear-state    Clear the recorded state for a session
  setup          Install hooks into an agent's config (e.g. setup claude)
  hook           Process a hook event end-to-end (used by hook scripts)
  version        Print version

Flags:
  --help, -h     Show this help
  --version, -v  Print version`)
}
