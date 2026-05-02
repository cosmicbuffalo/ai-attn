package main

import (
	"fmt"
	"os"
	"strings"
)

// isTerminal reports whether f is connected to an interactive terminal (used to enable ANSI colors).
func isTerminal(file *os.File) bool {
	fi, err := file.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// pad right-pads s with spaces to the given width. Used for columnar list output.
func padRight(text string, width int) string {
	if len(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-len(text))
}

// formatAge formats a duration in seconds as a human-readable short string (e.g., "5m", "2h").
func formatAge(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	} else if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	} else if seconds < 86400 {
		return fmt.Sprintf("%dh", seconds/3600)
	}
	return fmt.Sprintf("%dd", seconds/86400)
}
