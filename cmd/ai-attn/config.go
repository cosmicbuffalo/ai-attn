package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// defaultConfig returns a Config with sensible defaults (enabled, 72h TTL).
func defaultConfig() Config {
	return Config{
		Enabled:    true,
		TTLSeconds: 72 * 3600,
	}
}

// homeDir returns the current user's home directory, fatally exiting if it cannot be determined.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-attn: fatal: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	return home
}

// configPath returns the path to the config file, honoring the AI_ATTN_CONFIG env var.
// The default install does not require a config file — defaults apply when the file is
// missing.
func configPath() string {
	if v := os.Getenv("AI_ATTN_CONFIG"); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".config", "ai-attn", "config.toml")
}

// stateDir returns the directory for state files, honoring the AI_ATTN_STATE_DIR env var.
func stateDir() string {
	if v := os.Getenv("AI_ATTN_STATE_DIR"); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".local", "state", "ai-attn")
}

// ensureStateDir creates the state directory if it does not exist.
func ensureStateDir() error {
	return os.MkdirAll(stateDir(), 0o755)
}

// ensureAllDirs creates both the state directory and the config directory. Called by init-config.
func ensureAllDirs() error {
	if err := ensureStateDir(); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(configPath()), 0o755)
}

// loadConfig reads and parses the config file, falling back to defaults on error or missing file.
// A missing config file is the expected default-install state — there is no warning for it.
func loadConfig(stderr io.Writer) Config {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return defaultConfig()
	}
	cfg, err := parseConfig(data)
	if err != nil {
		fmt.Fprintf(stderr, "warning: invalid config %s: %s (using defaults)\n", configPath(), err)
		return defaultConfig()
	}
	return cfg
}

// parseConfig unmarshals TOML bytes into a Config, starting from defaults so missing keys
// inherit their default value rather than the Go zero value.
func parseConfig(data []byte) (Config, error) {
	cfg := defaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
