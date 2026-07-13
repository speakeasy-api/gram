// Package relay wires the agenthooks runtime to the Gram hooks backend. It
// builds an agenthooks.Runner whose handlers translate every coding-agent hook
// event into the canonical Gram ingest contract, POST it to the authenticated
// /rpc/hooks.ingest endpoint, and honor the server's allow/deny verdict. The
// server remains the sole authority on blocking; this package only relays
// events and enforces the returned decision.
package relay

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// DefaultServerURL is the production Gram endpoint used when no override is
// configured in the environment.
const DefaultServerURL = "https://app.getgram.ai"

// Config holds the deployment identity baked into a generated plugin. Every
// field has an environment override so a single binary can serve any
// project/org without recompilation, mirroring the env knobs the legacy bash
// senders read.
type Config struct {
	// ServerURL is the Gram API base, e.g. https://app.getgram.ai.
	ServerURL string
	// ProjectSlug routes events to a project via the Gram-Project header.
	ProjectSlug string
	// OrgID scopes the cached credential so a key minted for another org can
	// never authenticate this plugin (shared "default" slugs would otherwise
	// cross project boundaries).
	OrgID string
	// HooksAPIKey is the shared org-wide hooks key baked into a published
	// plugin. It is a fallback behind explicit and cached per-user keys and is
	// never persisted locally.
	HooksAPIKey string
	// BrowserLogin controls whether fresh machines may mint a per-user hooks
	// key through the dashboard. Published plugins default this off and use the
	// shared org key unless the organization opts in.
	BrowserLogin bool
	// Nonblocking swallows server deny decisions and turns transport failures
	// into allow. It mirrors the plugin's observability mode.
	Nonblocking bool
	// DebugLog, when set, appends one diagnostic line per event. It travels as a
	// command flag so it survives providers that scrub the hook environment.
	DebugLog string
	// ConfigPath records the speakeasy.json the config was loaded from, so the
	// login nudge can point the sign-in command at the same deployment identity
	// instead of the production defaults.
	ConfigPath string
	// ConfigError records a failure to read an explicitly referenced
	// speakeasy.json. The deployment identity is unknown, so events must not
	// fall back to the default server where a cached key could route them to
	// another workspace.
	ConfigError string
}

// FileConfig is the on-disk schema of a plugin's speakeasy.json. Evolution is
// strictly additive: readers ignore unknown keys and default missing ones, so
// a binary and a config file can skew in either direction. Never rename or
// repurpose a key.
type FileConfig struct {
	ServerURL    string `json:"server_url"`
	Project      string `json:"project"`
	Org          string `json:"org,omitempty"`
	HooksAPIKey  string `json:"hooks_api_key,omitempty"`
	BrowserLogin bool   `json:"browser_login,omitempty"`
	Nonblocking  bool   `json:"nonblocking,omitempty"`
}

// SplitInlineFlags extracts the deployment flags from a hook command's argv and
// returns the config they carry plus the remaining args to hand to agenthooks.
// The install packaging bakes only --config=<path>: deployment values
// must never ride argv because providers parse and filter hook commands
// (Cursor drops the whole hooks.json over a scheme:// URL, agenthooks quirk
// #29) and Codex trust-hashes the command definition. The remaining flags are
// manual overrides layered on top of the file.
func SplitInlineFlags(defaults Config, args []string) (Config, []string) {
	cfg := defaults
	overrides := Config{ServerURL: "", ProjectSlug: "", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	rest := make([]string, 0, len(args))
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--config="):
			cfg.ConfigPath = strings.TrimPrefix(a, "--config=")
			if fc, err := readFileConfig(cfg.ConfigPath); err == nil {
				cfg.ConfigError = ""
				cfg.ServerURL = fc.ServerURL
				cfg.ProjectSlug = fc.Project
				cfg.OrgID = fc.Org
				cfg.HooksAPIKey = fc.HooksAPIKey
				cfg.BrowserLogin = fc.BrowserLogin
				cfg.Nonblocking = fc.Nonblocking
			} else {
				cfg.ConfigError = err.Error()
			}
		case strings.HasPrefix(a, "--server-url="):
			overrides.ServerURL = strings.TrimPrefix(a, "--server-url=")
		case strings.HasPrefix(a, "--project="):
			overrides.ProjectSlug = strings.TrimPrefix(a, "--project=")
		case strings.HasPrefix(a, "--org="):
			overrides.OrgID = strings.TrimPrefix(a, "--org=")
		case a == "--nonblocking":
			overrides.Nonblocking = true
		case strings.HasPrefix(a, "--debug-log="):
			overrides.DebugLog = strings.TrimPrefix(a, "--debug-log=")
		default:
			rest = append(rest, a)
		}
	}
	if overrides.ServerURL != "" {
		cfg.ServerURL = overrides.ServerURL
	}
	if overrides.ProjectSlug != "" {
		cfg.ProjectSlug = overrides.ProjectSlug
	}
	if overrides.OrgID != "" {
		cfg.OrgID = overrides.OrgID
	}
	if overrides.Nonblocking {
		cfg.Nonblocking = true
	}
	if overrides.DebugLog != "" {
		cfg.DebugLog = overrides.DebugLog
	}
	return cfg, rest
}

// readFileConfig loads a speakeasy.json. A missing or malformed file yields an
// error so the caller keeps its defaults; hooks must not crash over config.
func readFileConfig(path string) (FileConfig, error) {
	var fc FileConfig
	b, err := os.ReadFile(path)
	if err != nil {
		return fc, err
	}
	if err := json.Unmarshal(b, &fc); err != nil {
		return fc, fmt.Errorf("parse %s: %w", path, err)
	}
	return fc, nil
}

// LoadConfig resolves the effective config from the environment, layering the
// GRAM_HOOKS_* overrides over the provided defaults.
func LoadConfig(defaults Config) Config {
	cfg := defaults
	if v := strings.TrimSpace(os.Getenv("GRAM_HOOKS_SERVER_URL")); v != "" {
		cfg.ServerURL = v
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = DefaultServerURL
	}
	if v := strings.TrimSpace(os.Getenv("GRAM_HOOKS_PROJECT_SLUG")); v != "" {
		cfg.ProjectSlug = v
	}
	if v := strings.TrimSpace(os.Getenv("GRAM_HOOKS_ORG_ID")); v != "" {
		cfg.OrgID = v
	}
	if v := strings.TrimSpace(os.Getenv("GRAM_HOOKS_ORG_KEY")); v != "" {
		cfg.HooksAPIKey = v
	}
	if v := strings.TrimSpace(os.Getenv("GRAM_HOOKS_BROWSER_LOGIN")); v != "" {
		cfg.BrowserLogin = v == "1" || strings.EqualFold(v, "true")
	}
	if os.Getenv("GRAM_HOOKS_NONBLOCKING") != "" || os.Getenv("GRAM_HOOKS_OBSERVABILITY_MODE") != "" {
		cfg.Nonblocking = true
	}
	cfg.ServerURL = strings.TrimRight(cfg.ServerURL, "/")
	return cfg
}
