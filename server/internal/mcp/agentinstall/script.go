// Package agentinstall renders the single-use shell script behind an agent's
// install command. The script connects the MCP clients it finds on a machine
// to the agent's gateway and, for OpenClaw, also installs the Speakeasy
// observability plugin so the agent's sessions are recorded under its name.
//
// The script carries the agent key, so every value interpolated into it is
// rendered as a POSIX single-quoted word, and the only other dynamic content
// is base64 inside a quoted heredoc.
package agentinstall

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// Params is everything the script needs about the agent and the deployment.
type Params struct {
	// GatewayURL is the agent's MCP gateway, /agent-mcp/<agent id>.
	GatewayURL string

	// Key is the agent's API key. The MCP clients send it to the gateway and
	// the OpenClaw hooks plugin sends it to the hooks API.
	Key string

	// ServerURL is the Speakeasy base URL the hooks plugin reports to.
	ServerURL string

	// OrganizationID pins the hooks plugin to the agent's organization, so a
	// cached sign-in for another organization is never used in its place.
	OrganizationID string

	// ProjectSlug is the project the agent's hooks are recorded in. Empty when
	// the agent has no live project, which leaves OpenClaw without hooks.
	ProjectSlug string

	// InstallFailOpen is the organization's hooks install-failure policy, as
	// the plugins page bakes it into every observability plugin.
	InstallFailOpen bool
}

// openClawMCPServerName is the name the script gives the gateway in
// OpenClaw's mcp.servers. OpenClaw prefixes the gateway's tools with it.
const openClawMCPServerName = "speakeasy"

// openClawMCPServer is one entry of OpenClaw's mcp.servers config, in the
// shape OpenClaw stores in mcp.servers.
type openClawMCPServer struct {
	// URL is the remote MCP endpoint.
	URL string `json:"url"`

	// Transport selects the MCP transport; the gateway speaks Streamable HTTP.
	Transport string `json:"transport"`

	// Headers are stored in OpenClaw's private config. Released OpenClaw
	// versions accept scalar MCP headers, not structured SecretRefs. The
	// installer passes this config through a private file, never argv.
	Headers map[string]string `json:"headers"`
}

// base64LineWidth wraps the embedded plugin like MIME base64, so the script
// stays readable in a pager and every decoder accepts it.
const base64LineWidth = 76

// noProjectReason is printed when the agent has no live project, either none
// at all or a deleted one: the hooks API records sessions per project, so
// there is nowhere to send them.
const noProjectReason = "this agent has no project, or its project was deleted, so Speakeasy has nowhere to record its sessions. " +
	"OpenClaw can use the gateway, but its sessions are not recorded."

// shellSingleQuote renders a value as a POSIX single-quoted word. The values
// interpolated into the script run on someone's machine, so none may end the
// quoting and start a command.
func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// Script renders the install script. It configures what it finds and says
// what it did; it never installs a client. A rejected client update keeps
// the existing gateway configuration in place.
func Script(p Params) (string, error) {
	for name, raw := range map[string]string{"gateway": p.GatewayURL, "hooks server": p.ServerURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return "", fmt.Errorf("%s URL must use HTTPS without user information", name)
		}
	}
	mcpServer, err := json.Marshal([]struct {
		Path  string            `json:"path"`
		Value openClawMCPServer `json:"value"`
	}{{Path: "mcp.servers." + openClawMCPServerName, Value: openClawMCPServer{
		URL:       p.GatewayURL,
		Transport: "streamable-http",
		Headers:   map[string]string{"Authorization": "Bearer " + p.Key},
	}}})
	if err != nil {
		return "", fmt.Errorf("marshal openclaw mcp server: %w", err)
	}

	plugin, err := openClawPlugin(p)
	if err != nil {
		return "", err
	}
	unavailable := ""
	if plugin == nil {
		unavailable = noProjectReason
	}

	var b strings.Builder
	b.WriteString(scriptHeader)
	b.WriteString("GRAM_AGENT_URL=" + shellSingleQuote(p.GatewayURL) + "\n")
	b.WriteString("SPEAKEASY_AGENT_KEY=" + shellSingleQuote(p.Key) + "\n")
	b.WriteString("export SPEAKEASY_AGENT_KEY\n")
	b.WriteString("# OpenClaw reads MCP headers from its own config, so its entry carries the key.\n")
	b.WriteString("OPENCLAW_MCP_CONFIG=" + shellSingleQuote(string(mcpServer)) + "\n")
	b.WriteString("# Why this script cannot install OpenClaw hooks; empty when it can.\n")
	b.WriteString("OPENCLAW_HOOKS_UNAVAILABLE=" + shellSingleQuote(unavailable) + "\n")
	b.WriteString(scriptBody)
	if plugin != nil {
		b.WriteString(pluginFunction(plugin))
	}
	b.WriteString("\nmain\n")
	return b.String(), nil
}

// openClawPlugin renders the agent's OpenClaw observability plugin as a ZIP,
// or nil when the agent has no project to record its sessions in.
func openClawPlugin(p Params) ([]byte, error) {
	if p.ProjectSlug == "" {
		return nil, nil
	}

	plugin, err := plugins.ObservabilityPluginZip(plugins.GenerateConfig{
		OrgName:          "",
		OrgEmail:         "",
		OrgID:            p.OrganizationID,
		ServerURL:        p.ServerURL,
		APIKey:           "",
		HooksAPIKey:      p.Key,
		ProjectSlug:      p.ProjectSlug,
		IsDefaultProject: false,
		Version:          "",
		MarketplaceName:  "",
		// An agent has no browser to sign in with; the baked key is its only
		// credential.
		BrowserLogin:    false,
		HooksKeyIsAgent: true,
		HooksOrgName:    "",
		InstallFailOpen: p.InstallFailOpen,
	}, "openclaw")
	if err != nil {
		return nil, fmt.Errorf("render openclaw observability plugin: %w", err)
	}
	return plugin, nil
}

// pluginFunction renders the shell function that prints the plugin ZIP as
// base64. The heredoc delimiter is quoted, so the shell expands nothing in it,
// and the base64 alphabet cannot spell the delimiter line.
func pluginFunction(plugin []byte) string {
	encoded := base64.StdEncoding.EncodeToString(plugin)

	var b strings.Builder
	b.WriteString("\n# The Speakeasy observability plugin for OpenClaw, a ZIP archive in base64.\n")
	b.WriteString("# Its speakeasy.json carries this agent's key and project.\n")
	b.WriteString("write_openclaw_plugin() {\n")
	b.WriteString("  cat <<'SPEAKEASY_OPENCLAW_PLUGIN'\n")
	for len(encoded) > base64LineWidth {
		b.WriteString(encoded[:base64LineWidth] + "\n")
		encoded = encoded[base64LineWidth:]
	}
	b.WriteString(encoded + "\n")
	b.WriteString("SPEAKEASY_OPENCLAW_PLUGIN\n")
	b.WriteString("}\n")
	return b.String()
}

const scriptHeader = `#!/bin/sh
# Connects this machine's MCP clients to a Speakeasy agent gateway. On
# OpenClaw it also installs Speakeasy observability hooks, so the agent's
# sessions are recorded under its name.
# Generated for one use. The key below is live: treat this file as a secret.
#
# Every step is a function and the last line runs them, so a download cut
# short defines a few functions and runs nothing.
set -eu
# The files this script writes carry the key: keep them owner-only.
umask 077

`

const scriptBody = `
configured=0
found=0
configure_claude_code() {
  # Keep the bearer out of argv and let Claude expand it when launched.
  if ! claude mcp add --transport http gram "$GRAM_AGENT_URL" \
    --header 'Authorization: Bearer ${SPEAKEASY_AGENT_KEY}'; then
    echo "Could not configure Claude Code. Its existing entry was kept; review it in the client before retrying. Continuing with the other clients."
    return 0
  fi
  echo "Configured Claude Code. Launch it with SPEAKEASY_AGENT_KEY set."
  configured=$((configured + 1))
}

configure_codex() {
  if ! codex mcp add gram --url "$GRAM_AGENT_URL" \
    --bearer-token-env-var SPEAKEASY_AGENT_KEY; then
    echo "Could not configure Codex. Its existing entry was kept; review it in the client before retrying. Continuing with the other clients."
    return 0
  fi
  echo "Configured Codex."
  configured=$((configured + 1))
}

configure_openclaw() {
  openclaw_changed=0
  add_openclaw_gateway
  # Hooks are independent of the gateway entry, so install them either way.
  install_openclaw_hooks
  if [ "$openclaw_changed" -eq 1 ]; then
    # Restart is explicit so installing hooks does not interrupt a running
    # Gateway or another active session.
    echo "Next: restart the OpenClaw Gateway. It does not hot-load plugins, so"
    echo "these changes take effect after the restart."
  fi
}

add_openclaw_gateway() {
  # Batch-file writes validate and replace the entry without exposing the key
  # in process arguments. A rejected update preserves the previous gateway.
  if ! openclaw config set --help 2>&1 | grep -q -e --batch-file; then
    echo "Could not configure the OpenClaw gateway: update OpenClaw to a release with config set --batch-file."
    return 0
  fi
  printf '%s\n' "$OPENCLAW_MCP_CONFIG" >"$workdir/mcp-config.json"
  if ! openclaw config set --batch-file "$workdir/mcp-config.json" >/dev/null; then
    echo "Could not add the gateway to OpenClaw (see its output above). Previous gateway entries were kept."
    return 0
  fi
  openclaw mcp unset gram >/dev/null 2>&1 || true
  echo "Configured OpenClaw."
  configured=$((configured + 1))
  openclaw_changed=1
}

# The agent that runs this script reports this line back, so it always
# starts the same way.
hooks_fail() {
  echo "Speakeasy hooks: FAIL: $1"
}

install_openclaw_hooks() {
  if [ -n "$OPENCLAW_HOOKS_UNAVAILABLE" ]; then
    hooks_fail "$OPENCLAW_HOOKS_UNAVAILABLE"
    return 0
  fi

  plugin="$workdir/speakeasy-observability-openclaw.zip"
  write_openclaw_plugin >"$workdir/plugin.b64"
  if ! decode_base64 "$workdir/plugin.b64" "$plugin"; then
    hooks_fail "could not decode the plugin. Install base64, python3 or node, then run a new install command."
    return 0
  fi

  # Installation flags vary across releases: use only those this CLI lists.
  if ! install_help=$(openclaw plugins install --help 2>&1); then
    hooks_fail "could not inspect OpenClaw plugin installation options."
    return 0
  fi
  set --
  if printf '%s' "$install_help" | grep -q -e --force; then
    set -- "$@" --force
  fi
  if printf '%s' "$install_help" | grep -q -e --accept-capabilities; then
    set -- "$@" --accept-capabilities
  fi
  if ! openclaw plugins install "$@" "$plugin"; then
    hooks_fail "openclaw plugins install did not succeed (see its output above)."
    return 0
  fi
  openclaw_changed=1
  # The installer may not keep the archive's owner-only mode on the file that
  # carries the key.
  installed_config="$(openclaw_plugin_dir)/speakeasy.json"
  if [ -f "$installed_config" ]; then
    if ! chmod 600 "$installed_config"; then
      hooks_fail "could not protect the installed key file. Restrict it to owner-only access before starting OpenClaw."
      return 0
    fi
  fi

  # Without this OpenClaw never hands the plugin prompts or replies.
  if ! openclaw config set plugins.entries.speakeasy-observability.hooks.allowConversationAccess true >/dev/null; then
    hooks_fail "could not allow the plugin conversation access. OpenClaw 2026.4.24 or later is required."
    return 0
  fi

  check_openclaw_hooks "$workdir" "$plugin"
}

# decode_base64 IN OUT. The decode flag differs between GNU and BSD base64,
# and a minimal image may lack it; OpenClaw itself always brings node.
decode_base64() {
  base64 -d <"$1" >"$2" 2>/dev/null && return 0
  base64 -D <"$1" >"$2" 2>/dev/null && return 0
  python3 -I -c 'import base64, sys; sys.stdout.buffer.write(base64.b64decode(sys.stdin.read()))' <"$1" >"$2" 2>/dev/null && return 0
  node -e 'let s = ""; process.stdin.on("data", (d) => (s += d)).on("end", () => process.stdout.write(Buffer.from(s, "base64")))' <"$1" >"$2" 2>/dev/null
}

# extract_zip ZIP DIR. Minimal OpenClaw images may lack unzip but carry
# bsdtar or python3 can extract the same ZIP on those systems.
extract_zip() {
  mkdir -p "$2"
  unzip -q "$1" -d "$2" >/dev/null 2>&1 && return 0
  bsdtar -xf "$1" -C "$2" >/dev/null 2>&1 && return 0
  python3 -I -c 'import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])' "$1" "$2" >/dev/null 2>&1
}

# Where OpenClaw keeps an installed plugin: the extensions directory of its
# state directory.
openclaw_plugin_dir() {
  if [ -n "${OPENCLAW_STATE_DIR:-}" ]; then
    echo "$OPENCLAW_STATE_DIR/extensions/speakeasy-observability"
    return 0
  fi
  if [ -n "${OPENCLAW_CONFIG_PATH:-}" ]; then
    echo "$(dirname "$OPENCLAW_CONFIG_PATH")/extensions/speakeasy-observability"
    return 0
  fi
  if [ -n "${OPENCLAW_PROFILE:-}" ] && [ "$OPENCLAW_PROFILE" != "default" ]; then
    echo "$HOME/.openclaw-$OPENCLAW_PROFILE/extensions/speakeasy-observability"
    return 0
  fi
  echo "$HOME/.openclaw/extensions/speakeasy-observability"
}

# check_openclaw_hooks WORKDIR ZIP sends one tool call through the plugin's
# hooks binary, the way OpenClaw does, and reports Speakeasy's verdict.
check_openclaw_hooks() {
  plugin_dir=$(openclaw_plugin_dir)
  if [ ! -r "$plugin_dir/speakeasy.json" ]; then
    # Not where this release keeps plugins: test the archive's own copy.
    plugin_dir="$1/plugin"
    if ! extract_zip "$2" "$plugin_dir"; then
      hooks_fail "the plugin is installed, but this script could not find it to test it. Install unzip, bsdtar or python3 to test the archive instead."
      return 0
    fi
  fi
  if ! command -v bash >/dev/null 2>&1; then
    hooks_fail "the plugin runs its hooks with bash, which is not on PATH."
    return 0
  fi

  frame='{"seq":1,"hook":"before_tool_call","event":{"toolName":"exec","toolCallId":"speakeasy-smoke-1","params":{"command":"true"}},"ctx":{"sessionId":"speakeasy-smoke-test"},"timeoutMs":10000}'
  # Test the key baked into the plugin: an empty key variable counts as unset,
  # and the sign-in cache moves to this script's directory, where there is
  # none. A machine with no cached org settings lets a call through when
  # Speakeasy is unreachable; the placeholder settings file below makes an
  # unreachable server a refusal, so only a real answer can pass.
  printf '{}\n' >"$1/hooks-auth.env.org-settings.json"
  reply=$(printf '%s\n' "$frame" |
    SPEAKEASY_AI_HOOKS_API_KEY='' GRAM_HOOKS_API_KEY='' \
      SPEAKEASY_AI_HOOKS_ORG_KEY='' GRAM_HOOKS_ORG_KEY='' \
      SPEAKEASY_AI_HOOKS_FAIL_OPEN='' GRAM_HOOKS_FAIL_OPEN='' \
      SPEAKEASY_AI_HOOKS_AUTH_FILE="$1/hooks-auth.env" \
      bash "$plugin_dir/hooks/bootstrap.sh" agenthooks serve --provider=openclaw --timeout=15s \
      "--config=$plugin_dir/speakeasy.json" 2>"$1/hooks.log") || true

  case "$reply" in
    *'"seq":1'*) ;;
    *)
      hooks_fail "the hooks binary did not answer. $(tail -n 2 "$1/hooks.log" 2>/dev/null | tr '\n' ' ')"
      return 0
      ;;
  esac
  case "$reply" in
    *'"block"'* | *'"error"'*) ;;
    *)
      echo "Speakeasy hooks: PASS"
      note_credential_overrides
      return 0
      ;;
  esac

  reason=$(printf '%s\n' "$reply" | sed -n 's/.*"blockReason":"\([^"]*\)".*/\1/p')
  if [ -z "$reason" ]; then
    reason=$(printf '%s\n' "$reply" | sed -n 's/.*"error":"\([^"]*\)".*/\1/p')
  fi
  hooks_fail "${reason:-Speakeasy refused the test tool call.}"
  case "$reason" in
    *"permission denied"* | *[Ff]orbidden* | *"HTTP 403"*)
      echo "This agent's key may not send hooks yet. It needs org:hooks_ingest on the"
      echo "agent's project, and project:read. Ask an admin to grant both, then run a"
      echo "new install command."
      ;;
    *"unauthorized"* | *"HTTP 401"*)
      echo "Speakeasy did not accept this agent's key. It may be revoked or expired:"
      echo "issue a new key and run a new install command."
      ;;
  esac
}

# The hooks binary prefers these credentials to the plugin's key, and OpenClaw
# sessions would then be recorded under them instead of this agent.
note_credential_overrides() {
  if [ -n "${SPEAKEASY_AI_HOOKS_API_KEY:-}${GRAM_HOOKS_API_KEY:-}" ]; then
    echo "Note: SPEAKEASY_AI_HOOKS_API_KEY is set. If the OpenClaw Gateway inherits it,"
    echo "it is used instead of this agent's key."
  fi
  if [ -n "${SPEAKEASY_AI_HOOKS_ORG_KEY:-}${GRAM_HOOKS_ORG_KEY:-}" ]; then
    echo "Note: SPEAKEASY_AI_HOOKS_ORG_KEY is set. If the OpenClaw Gateway inherits it,"
    echo "it is used instead of this agent's key."
  fi
}

main() {
  workdir=$(mktemp -d)
  trap 'rm -rf "$workdir"' EXIT
  if command -v claude >/dev/null 2>&1; then
    found=$((found + 1))
    configure_claude_code
  fi
  if command -v codex >/dev/null 2>&1; then
    found=$((found + 1))
    configure_codex
  fi
  if command -v openclaw >/dev/null 2>&1; then
    found=$((found + 1))
    configure_openclaw
  fi

  if [ "$configured" -eq 0 ]; then
    if [ "$found" -eq 0 ]; then
      echo "No supported MCP client (Claude Code, Codex, OpenClaw) found on PATH."
    else
      echo "Supported clients were found, but no gateway was configured. Review the errors above; hooks may have been installed separately."
    fi
    echo "Connect one manually with:"
    echo "  URL:    $GRAM_AGENT_URL"
    echo "  Header: Authorization: Bearer \$SPEAKEASY_AGENT_KEY"
    echo
    echo "SPEAKEASY_AGENT_KEY is set in this shell only. Store it before closing."
    exit 1
  fi

  echo
  echo "Done. SPEAKEASY_AGENT_KEY was exported for this run only. Store it if a"
  echo "client you use reads it from the environment."
}
`
