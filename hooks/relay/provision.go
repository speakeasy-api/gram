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
	ServerURL   string
	ProjectSlug string
	OrgID       string
	BinaryPath  string
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
	return install.Install(ctx, manifest(cfg, dir), target)
}

// manifest declares the event subscriptions every provider config is rendered
// from. Deployment values must never ride argv (providers parse and filter
// hook commands — Cursor drops the whole hooks.json over a scheme:// URL,
// quirk #29 — and Codex trust-hashes the command definition), so the command
// carries only a pointer to the plugin's config file.
func manifest(cfg PluginConfig, dir string) install.Manifest {
	return InstallManifest(
		[]string{cfg.BinaryPath, "--config=" + filepath.Join(dir, configFileName)},
		install.Identity{Name: pluginName, Version: "0.0.0", Description: pluginDescription},
	)
}

// InstallManifest is the single subscription table every speakeasy-hooks
// provider config is rendered from. Both the local `speakeasy-hooks install`
// path and the server's plugin distribution render from it, so what ships to
// the fleet is what the e2e harness exercises locally.
func InstallManifest(command []string, id install.Identity) install.Manifest {
	gate := func(kind agenthooks.EventKind, timeout time.Duration) install.HookSpec {
		return install.HookSpec{Kind: kind, Tools: install.ToolMatcher{}, Blocking: true, Timeout: timeout}
	}
	observe := func(kind agenthooks.EventKind) install.HookSpec {
		return install.HookSpec{Kind: kind, Tools: install.ToolMatcher{}, Blocking: false, Timeout: 60 * time.Second}
	}
	return install.Manifest{
		Command: command,
		Hooks: []install.HookSpec{
			// SessionStart runs the interactive sign-in preflight; the raised
			// timeout covers the browser round-trip.
			gate(agenthooks.KindSessionStart, 330*time.Second),
			gate(agenthooks.KindPromptSubmitted, 60*time.Second),
			gate(agenthooks.KindToolPre, 60*time.Second),
			gate(agenthooks.KindPermission, 60*time.Second),
			observe(agenthooks.KindToolPost),
			observe(agenthooks.KindToolError),
			observe(agenthooks.KindStop),
			observe(agenthooks.KindSessionEnd),
			observe(agenthooks.KindNotification),
			observe(agenthooks.KindModelResponse),
		},
		Identity: id,
		// Crashed decision hooks must not silently allow (cursor failClosed,
		// quirk #7); the ratchet's fail-open posture for unauthenticated
		// machines is handled in-process, not by the provider.
		Fail: agenthooks.FailClosed,
	}
}

// writeConfigFile renders the plugin's speakeasy.json. Schema evolution is
// strictly additive: unknown keys are ignored on read and missing keys
// default, so binary and config can skew in either direction.
func writeConfigFile(dir string, cfg PluginConfig) error {
	return writeJSON(filepath.Join(dir, configFileName), FileConfig{
		ServerURL:   cfg.ServerURL,
		Project:     cfg.ProjectSlug,
		Org:         cfg.OrgID,
		Nonblocking: false,
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
