package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/install"

	"github.com/speakeasy-api/gram/hooks/relay"
)

// hooksShimSentinel stands in for the hook command while agenthooks renders a
// provider config. agenthooks shell-quotes command arguments for the host OS,
// which would single-quote a `$CLAUDE_PLUGIN_ROOT`-style path and defeat the
// provider's variable expansion — so rendering uses this quoting-neutral
// token and the generator splices the real shim invocation in afterwards.
const hooksShimSentinel = "__SPEAKEASY_HOOKS_SHIM__"

// renderBinaryProviderFiles renders a provider hook config from the same
// relay.InstallManifest that `speakeasy-hooks install` uses locally, so the
// subscriptions, timeouts, and fail modes shipped by the server are exactly
// what the hooks e2e harness exercises.
func renderBinaryProviderFiles(provider agenthooks.Provider, scope install.Scope, name string, cfg GenerateConfig) (map[string][]byte, error) {
	m := relay.InstallManifest([]string{hooksShimSentinel}, install.Identity{
		Name:        name,
		Version:     pluginManifestVersion(cfg),
		Description: "Speakeasy observability hooks for " + cfg.OrgName + ".",
	})
	fsys, err := install.Render(m, install.Target{Provider: provider, Scope: scope, Dir: ""})
	if err != nil {
		return nil, fmt.Errorf("render %s hooks config: %w", provider, err)
	}
	out := map[string][]byte{}
	if err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("read rendered file %s: %w", p, err)
		}
		out[p] = b
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read rendered %s hooks config: %w", provider, err)
	}
	return out, nil
}

// rewriteShimCommands splices the provider-specific shim invocation over the
// rendering sentinel. shimCommandJSON must already be escaped for a JSON
// string context (i.e. its double quotes written as \").
func rewriteShimCommands(raw []byte, shimCommandJSON string) ([]byte, error) {
	if !bytes.Contains(raw, []byte(hooksShimSentinel)) {
		return nil, fmt.Errorf("rendered hooks config contains no shim sentinel")
	}
	out := bytes.ReplaceAll(raw, []byte(hooksShimSentinel), []byte(shimCommandJSON))
	if !json.Valid(out) {
		return nil, fmt.Errorf("hooks config is not valid JSON after shim rewrite")
	}
	return out, nil
}

// renderSpeakeasyConfigFile emits the plugin's speakeasy.json deployment
// identity, byte-identical to what `speakeasy-hooks install` writes locally.
// The binary's Nonblocking switch carries the org's observability mode.
func renderSpeakeasyConfigFile(cfg GenerateConfig) ([]byte, error) {
	b, err := marshalJSON(relay.FileConfig{
		ServerURL:   strings.TrimRight(cfg.ServerURL, "/"),
		Project:     cfg.ProjectSlug,
		Org:         cfg.OrgID,
		Nonblocking: cfg.ObservabilityMode,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal speakeasy.json: %w", err)
	}
	return append(b, '\n'), nil
}

// generateClaudeObservabilityBinaryPluginInDir emits the alpha-channel
// variant of the Claude observability plugin: an agenthooks-rendered
// hooks.json whose commands drive the pinned speakeasy-hooks binary through
// the bootstrap shim.
func generateClaudeObservabilityBinaryPluginInDir(files map[string][]byte, subdir string, cfg GenerateConfig, pin hooksBinaryPin) error {
	if err := pin.validate(); err != nil {
		return fmt.Errorf("claude observability plugin: %w", err)
	}
	name := subdir
	if name == "" {
		name = ClaudeObservabilitySlug(cfg)
	}

	rendered, err := renderBinaryProviderFiles(agenthooks.ProviderClaudeCode, install.ScopePlugin, name, cfg)
	if err != nil {
		return err
	}
	hooksJSON, err := rewriteShimCommands(rendered["hooks/hooks.json"], `sh \"$CLAUDE_PLUGIN_ROOT/hooks/run.sh\"`)
	if err != nil {
		return fmt.Errorf("rewrite claude hooks.json: %w", err)
	}

	// The agenthooks-rendered plugin.json is minimal; keep the richer manifest
	// the stable channel publishes so marketplace listings look identical
	// across channels.
	pluginJSON, err := marshalJSON(claudePluginMeta{
		Name:        name,
		DisplayName: cfg.OrgName + " Observability",
		Description: "Speakeasy observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Speakeasy dashboard.",
		Version:     pluginManifestVersion(cfg),
		Author:      pluginAuthor{Name: cfg.OrgName, URL: "https://getgram.ai"},
		Homepage:    "https://getgram.ai",
		UserConfig:  nil,
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}

	speakeasyJSON, err := renderSpeakeasyConfigFile(cfg)
	if err != nil {
		return err
	}

	files[path.Join(subdir, ".claude-plugin/plugin.json")] = pluginJSON
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON
	files[path.Join(subdir, "hooks/run.sh")] = renderHooksBinaryShim("claude", pin)
	files[path.Join(subdir, "speakeasy.json")] = speakeasyJSON
	return nil
}

// generateCursorObservabilityBinaryPluginInDir is the Cursor alpha variant.
func generateCursorObservabilityBinaryPluginInDir(files map[string][]byte, subdir, name string, cfg GenerateConfig, pin hooksBinaryPin) error {
	if err := pin.validate(); err != nil {
		return fmt.Errorf("cursor observability plugin: %w", err)
	}

	rendered, err := renderBinaryProviderFiles(agenthooks.ProviderCursor, install.ScopePlugin, name, cfg)
	if err != nil {
		return err
	}
	hooksJSON, err := rewriteShimCommands(rendered["hooks/hooks.json"], `sh \"$CURSOR_PLUGIN_ROOT/hooks/run.sh\"`)
	if err != nil {
		return fmt.Errorf("rewrite cursor hooks.json: %w", err)
	}

	pluginJSON, err := marshalJSON(cursorPluginMeta{
		Name:        name,
		DisplayName: "Observability (Cursor)",
		Description: "Speakeasy observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Speakeasy dashboard.",
		Version:     pluginManifestVersion(cfg),
		Author:      cursorAuthor{Name: cfg.OrgName, Email: cfg.OrgEmail},
		Homepage:    "https://getgram.ai",
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}

	speakeasyJSON, err := renderSpeakeasyConfigFile(cfg)
	if err != nil {
		return err
	}

	files[path.Join(subdir, ".cursor-plugin/plugin.json")] = pluginJSON
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON
	files[path.Join(subdir, "hooks/run.sh")] = renderHooksBinaryShim("cursor", pin)
	files[path.Join(subdir, "speakeasy.json")] = speakeasyJSON
	return nil
}

// generateCodexObservabilityBinaryPluginInDir is the Codex alpha variant.
// agenthooks renders Codex configs for a home-directory install (flat
// hooks.json plus a config.toml trust seed), so the generator relocates the
// hooks config into the marketplace plugin layout and leaves trust seeding to
// the install script's pre-approvals, same as the stable channel.
func generateCodexObservabilityBinaryPluginInDir(files map[string][]byte, subdir string, cfg GenerateConfig, pin hooksBinaryPin) error {
	if err := pin.validate(); err != nil {
		return fmt.Errorf("codex observability plugin: %w", err)
	}
	name := subdir
	if name == "" {
		name = CodexObservabilitySlug(cfg)
	}

	hooksJSON, err := renderCodexBinaryHooksJSON(cfg)
	if err != nil {
		return err
	}

	pluginJSON, err := marshalJSON(codexPluginMeta{
		Name:        name,
		Version:     pluginManifestVersion(cfg),
		Description: "Speakeasy observability hooks for " + cfg.OrgName + ". Install this plugin to forward tool events to your team's Speakeasy dashboard.",
		MCPServers:  "",
		Hooks:       "./hooks/hooks.json",
		Interface: &codexInterface{
			DisplayName:      "Observability (Codex)",
			ShortDescription: "Speakeasy observability hooks",
		},
	})
	if err != nil {
		return fmt.Errorf("marshal plugin.json: %w", err)
	}

	speakeasyJSON, err := renderSpeakeasyConfigFile(cfg)
	if err != nil {
		return err
	}

	files[path.Join(subdir, ".codex-plugin/plugin.json")] = pluginJSON
	files[path.Join(subdir, "hooks/hooks.json")] = hooksJSON
	files[path.Join(subdir, "hooks/run.sh")] = renderHooksBinaryShim("codex", pin)
	files[path.Join(subdir, "speakeasy.json")] = speakeasyJSON
	return nil
}

// renderCodexBinaryHooksJSON renders the Codex alpha hooks.json. Shared by
// the plugin generator and the install-script approval computation so the
// trusted hashes are always derived from the exact bytes that ship.
func renderCodexBinaryHooksJSON(cfg GenerateConfig) ([]byte, error) {
	marketplace := resolveMarketplaceName(cfg)
	plugin := CodexObservabilitySlug(cfg)
	rendered, err := renderBinaryProviderFiles(agenthooks.ProviderCodex, install.ScopeUser, plugin, cfg)
	if err != nil {
		return nil, err
	}
	// Codex hooks run from the marketplace cache, not a plugin-root variable;
	// $HOME stays literal in the command so the trust hash is deterministic.
	shim := fmt.Sprintf(`sh \"$HOME/.codex/.tmp/marketplaces/%s/%s/hooks/run.sh\"`, marketplace, plugin)
	hooksJSON, err := rewriteShimCommands(rendered["hooks.json"], shim)
	if err != nil {
		return nil, fmt.Errorf("rewrite codex hooks.json: %w", err)
	}
	return hooksJSON, nil
}

// computeCodexBinaryHookApprovals derives the [hooks.state] pre-approvals for
// the alpha-channel Codex plugin from its rendered hooks.json. The rendered
// config has one matcher group with one handler per event, which is the shape
// install.DefinitionHash fingerprints.
func computeCodexBinaryHookApprovals(cfg GenerateConfig) ([]codexHookApproval, error) {
	marketplace := resolveMarketplaceName(cfg)
	plugin := CodexObservabilitySlug(cfg)
	hooksJSON, err := renderCodexBinaryHooksJSON(cfg)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(hooksJSON, &parsed); err != nil {
		return nil, fmt.Errorf("parse codex hooks.json: %w", err)
	}

	events := make([]string, 0, len(parsed.Hooks))
	for event := range parsed.Hooks {
		events = append(events, event)
	}
	sort.Strings(events)

	var approvals []codexHookApproval
	for _, event := range events {
		for groupIndex, group := range parsed.Hooks[event] {
			for hookIndex, hook := range group.Hooks {
				approvals = append(approvals, codexHookApproval{
					StateKey:    fmt.Sprintf("%s@%s:hooks/hooks.json:%s:%d:%d", plugin, marketplace, codexEventSnakeCase(event), groupIndex, hookIndex),
					TrustedHash: install.DefinitionHash(event, group.Matcher, hook.Command, hook.Timeout),
				})
			}
		}
	}
	return approvals, nil
}

// hooksBinaryShimTemplate is the bootstrap shim shipped as hooks/run.sh in
// alpha-channel plugins. It installs the pinned speakeasy-hooks release on
// first use and execs it with the plugin's deployment identity. A machine
// that has never bootstrapped fails open — a download outage may delay
// observability but must never block the user's agent (the authenticated
// fail-closed ratchet lives in the binary itself, which needs no network once
// installed).
const hooksBinaryShimTemplate = `#!/bin/sh
# Generated by Speakeasy. Do not edit - overwritten on every publish.
set -u

VERSION="{{VERSION}}"
PROVIDER="{{PROVIDER}}"

warn() { printf 'speakeasy-hooks: %s\n' "$1" >&2; }

fail_open() {
  warn "$1"
  if [ "$PROVIDER" = "cursor" ]; then
    printf '{}'
  fi
  exit 0
}

plugin_root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)" || plugin_root=""
[ -n "$plugin_root" ] || fail_open "cannot resolve plugin directory"

bin_dir="${XDG_DATA_HOME:-$HOME/.local/share}/speakeasy/hooks/${VERSION}"
bin="${bin_dir}/speakeasy-hooks"

if [ ! -x "$bin" ]; then
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
  darwin | linux) ;;
  *) fail_open "unsupported OS: $os" ;;
  esac
  arch="$(uname -m)"
  case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) fail_open "unsupported architecture: $arch" ;;
  esac
  case "${os}_${arch}" in
  darwin_amd64) want_sha="{{SHA_DARWIN_AMD64}}" ;;
  darwin_arm64) want_sha="{{SHA_DARWIN_ARM64}}" ;;
  linux_amd64) want_sha="{{SHA_LINUX_AMD64}}" ;;
  linux_arm64) want_sha="{{SHA_LINUX_ARM64}}" ;;
  *) fail_open "no checksum for ${os}_${arch}" ;;
  esac
  command -v curl >/dev/null 2>&1 || fail_open "curl is required to bootstrap speakeasy-hooks"
  command -v unzip >/dev/null 2>&1 || fail_open "unzip is required to bootstrap speakeasy-hooks"
  tmp="$(mktemp -d)" || fail_open "mktemp failed"
  trap 'rm -rf "$tmp"' EXIT
  archive="speakeasy-hooks_${os}_${arch}.zip"
  url="https://github.com/speakeasy-api/gram/releases/download/hooks%40${VERSION}/${archive}"
  curl -fsSL --retry 2 --max-time 120 -o "${tmp}/${archive}" "$url" || fail_open "download failed: ${url}"
  if command -v sha256sum >/dev/null 2>&1; then
    got_sha="$(sha256sum "${tmp}/${archive}" | cut -d' ' -f1)"
  else
    got_sha="$(shasum -a 256 "${tmp}/${archive}" | cut -d' ' -f1)" || fail_open "no sha256 tool available"
  fi
  [ "$got_sha" = "$want_sha" ] || fail_open "checksum mismatch for ${archive}"
  unzip -q -o "${tmp}/${archive}" -d "${tmp}/extracted" || fail_open "unzip failed"
  binary_path="$(find "${tmp}/extracted" -name speakeasy-hooks -type f | head -1)"
  [ -n "$binary_path" ] || fail_open "archive did not contain speakeasy-hooks"
  chmod +x "$binary_path" || fail_open "cannot mark binary executable"
  mkdir -p "$bin_dir" || fail_open "cannot create ${bin_dir}"
  staging="${bin}.partial.$$"
  cp "$binary_path" "$staging" || fail_open "cannot stage binary"
  mv "$staging" "$bin" || fail_open "cannot install binary"
  rm -rf "$tmp"
  trap - EXIT
fi

exec "$bin" --config="${plugin_root}/speakeasy.json" "$@"
`

func renderHooksBinaryShim(provider string, pin hooksBinaryPin) []byte {
	return []byte(strings.NewReplacer(
		"{{VERSION}}", pin.Version,
		"{{PROVIDER}}", provider,
		"{{SHA_DARWIN_AMD64}}", pin.Checksums["darwin_amd64"],
		"{{SHA_DARWIN_ARM64}}", pin.Checksums["darwin_arm64"],
		"{{SHA_LINUX_AMD64}}", pin.Checksums["linux_amd64"],
		"{{SHA_LINUX_ARM64}}", pin.Checksums["linux_arm64"],
	).Replace(hooksBinaryShimTemplate))
}
