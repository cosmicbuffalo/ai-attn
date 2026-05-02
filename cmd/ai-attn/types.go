package main

type Config struct {
	Enabled    bool `toml:"enabled"`
	TTLSeconds int  `toml:"ttl_seconds"`
}

type Record struct {
	Agent      string `json:"agent"`
	SessionKey string `json:"session_key"`
	State      string `json:"state"`
	Reason     string `json:"reason"`
	UpdatedAt  int64  `json:"updated_at"`
	AgeSeconds int64  `json:"age_seconds,omitempty"`
	CWD        string `json:"cwd"`
	SessionID  string `json:"session_id"`
	PaneID     string `json:"pane_id"`
}

type listPayload struct {
	GeneratedAt int64    `json:"generated_at"`
	TTLSeconds  int      `json:"ttl_seconds"`
	Records     []Record `json:"records"`
}

type sessionIdentity struct {
	Agent     string
	CWD       string
	SessionID string
	PaneID    string
}

var version = "dev"

// Exit codes returned by subcommands.
const (
	exitOK    = 0 // success
	exitError = 1 // runtime error (filesystem, config, state)
	exitUsage = 2 // usage error (missing flag, unknown command/agent)
)
