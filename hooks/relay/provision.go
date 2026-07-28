package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/install"
)

// Provider packaging. A single speakeasy-hooks binary replaces the per-provider
// bash senders; agenthooks/install renders the provider configs (with the
// per-provider quirk handling: cursor failClosed and URL rejection, claude
// async rules, codex --async and trust hashes), and gram adds a speakeasy.json
// carrying the deployment identity. This mirrors what the server will later
// emit for distribution; the install subcommand produces the same layout
// locally for tests.

// PluginConfig carries the deployment identity written into a provider package.
type PluginConfig struct {
	ServerURL    string
	ProjectSlug  string
	OrgID        string
	HooksAPIKey  string
	BrowserLogin bool
	BinaryPath   string
}

const pluginName = "speakeasy-observability"

const pluginDescription = "Speakeasy observability hooks."

// configFileName is the deployment-identity file written into every plugin
// package and referenced by each hook command via --config.
const configFileName = "speakeasy.json"

// WritePlugin renders a provider hook package under dir that drives the
// speakeasy-hooks binary. provider is the agenthooks slug (claude-code,
// cursor, codex). For claude-code and cursor, dir is a plugin directory; for
// codex, which has no plugin layout for hooks, dir is the Codex home the
// config installs into.
func WritePlugin(ctx context.Context, provider, dir string, cfg PluginConfig) error {
	var target install.Target
	switch provider {
	case "claude-code", "claude":
		target = install.Target{Provider: agenthooks.ProviderClaudeCode, Scope: install.ScopePlugin, Dir: dir}
	case "cursor":
		target = install.Target{Provider: agenthooks.ProviderCursor, Scope: install.ScopePlugin, Dir: dir}
	case "codex":
		target = install.Target{Provider: agenthooks.ProviderCodex, Scope: install.ScopeUser, Dir: dir}
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}
	if err := writeConfigFile(dir, cfg); err != nil {
		return err
	}
	if err := install.Install(ctx, manifest(target.Provider, cfg, dir), target); err != nil {
		return err
	}
	return alignPublishedHookEvents(target.Provider, dir)
}

// manifest declares the event subscriptions every provider config is rendered
// from. Deployment values must never ride argv (providers parse and filter
// hook commands — Cursor drops the whole hooks.json over a scheme:// URL,
// quirk #29 — and Codex trust-hashes the command definition), so the command
// carries only a pointer to the plugin's config file.
func manifest(provider agenthooks.Provider, cfg PluginConfig, dir string) install.Manifest {
	gate := func(kind agenthooks.EventKind, timeout time.Duration) install.HookSpec {
		return install.HookSpec{Kind: kind, Tools: install.ToolMatcher{}, Blocking: true, Timeout: timeout}
	}
	observe := func(kind agenthooks.EventKind) install.HookSpec {
		return install.HookSpec{Kind: kind, Tools: install.ToolMatcher{}, Blocking: false, Timeout: 60 * time.Second}
	}
	hooks := []install.HookSpec{
		gate(agenthooks.KindSessionStart, 330*time.Second),
		gate(agenthooks.KindPromptSubmitted, 60*time.Second),
		gate(agenthooks.KindToolPre, 60*time.Second),
		observe(agenthooks.KindToolPost),
		observe(agenthooks.KindToolError),
		gate(agenthooks.KindStop, 60*time.Second),
		observe(agenthooks.KindSessionEnd),
		observe(agenthooks.KindNotification),
		observe(agenthooks.KindModelResponse),
	}
	if provider == agenthooks.ProviderCodex {
		hooks = append(hooks, gate(agenthooks.KindPermission, 60*time.Second))
	}
	return install.Manifest{
		Command:  []string{cfg.BinaryPath, "--config=" + filepath.Join(dir, configFileName)},
		Hooks:    hooks,
		Identity: install.Identity{Name: pluginName, Version: "0.0.0", Description: pluginDescription},
		// Crashed decision hooks must not silently allow (cursor failClosed,
		// quirk #7); the ratchet's fail-open posture for unauthenticated
		// machines is handled in-process, not by the provider.
		Fail: agenthooks.FailClosed,
	}
}

// alignPublishedHookEvents trims agenthooks' broader provider defaults to the
// exact event set used by the released Bash plugins. ConfigChange has no
// unified agenthooks kind yet, so Claude receives a copy of the asynchronous
// Notification command and the runner observes it through OnOther.
func alignPublishedHookEvents(provider agenthooks.Provider, dir string) error {
	var path string
	switch provider {
	case agenthooks.ProviderClaudeCode:
		path = filepath.Join(dir, "hooks", "hooks.json")
	case agenthooks.ProviderCursor:
		path = filepath.Join(dir, "hooks", "hooks.json")
	default:
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc struct {
		Version int                        `json:"version,omitempty"`
		Hooks   map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("parse generated hooks config: %w", err)
	}
	switch provider {
	case agenthooks.ProviderClaudeCode:
		notification, ok := doc.Hooks["Notification"]
		if !ok {
			return fmt.Errorf("generated Claude hooks config is missing Notification")
		}
		doc.Hooks["ConfigChange"] = append(json.RawMessage(nil), notification...)
	case agenthooks.ProviderCursor:
		for _, event := range []string{"beforeShellExecution", "beforeReadFile", "afterShellExecution"} {
			delete(doc.Hooks, event)
		}
		var entries []map[string]any
		if err := json.Unmarshal(doc.Hooks["sessionStart"], &entries); err != nil {
			return fmt.Errorf("parse generated Cursor sessionStart hooks: %w", err)
		}
		for _, entry := range entries {
			entry["failClosed"] = true
		}
		doc.Hooks["sessionStart"], err = json.Marshal(entries)
		if err != nil {
			return fmt.Errorf("marshal Cursor sessionStart hooks: %w", err)
		}
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal aligned hooks config: %w", err)
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// writeConfigFile renders the plugin's speakeasy.json. Schema evolution is
// strictly additive: unknown keys are ignored on read and missing keys
// default, so binary and config can skew in either direction.
func writeConfigFile(dir string, cfg PluginConfig) error {
	return writeJSON(filepath.Join(dir, configFileName), FileConfig{
		ServerURL:    cfg.ServerURL,
		Project:      cfg.ProjectSlug,
		Org:          cfg.OrgID,
		HooksAPIKey:  cfg.HooksAPIKey,
		BrowserLogin: cfg.BrowserLogin,
		Nonblocking:  false,
	})
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
