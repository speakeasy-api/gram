package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// deviceAgentRunMode is how the device agent runs on an agent's host.
type deviceAgentRunMode string

const (
	// deviceAgentRunModeEphemeral reconciles once and exits, for hosts that
	// live for one session and rerun setup at the start of the next.
	deviceAgentRunModeEphemeral deviceAgentRunMode = "ephemeral"

	// deviceAgentRunModeService installs a per-user background service, for
	// hosts that stay up.
	deviceAgentRunModeService deviceAgentRunMode = "service"
)

// deviceAgentRunModes lists the modes an install code may carry.
var deviceAgentRunModes = []deviceAgentRunMode{deviceAgentRunModeEphemeral, deviceAgentRunModeService}

const (
	// deviceAgentInstallerURL is the hosted installer. It resolves the latest
	// stable release and verifies its checksum, so the script pins neither.
	deviceAgentInstallerURL = "https://storage.googleapis.com/speakeasy-device-agent-releases-prod/install.sh"

	// deviceAgentManagedConfigPath is where the device agent reads managed
	// enrollment on Linux. Agent identities run on Linux hosts; macOS hosts
	// get the signed package through MDM instead.
	deviceAgentManagedConfigPath = "/etc/speakeasy/managed.json"

	// deviceAgentDefaultControlPlane is the control plane the device agent
	// reports to when managed enrollment names none.
	deviceAgentDefaultControlPlane = "https://app.getgram.ai"

	// deviceAgentManagedConfigVersion is the managed enrollment schema version.
	deviceAgentManagedConfigVersion = 1
)

// deviceAgentManagedConfig is the managed enrollment file the device agent
// reads at deviceAgentManagedConfigPath. Its shape is the device agent's
// contract.
type deviceAgentManagedConfig struct {
	// V is the schema version.
	V int `json:"v"`

	// AgentKey is the agent API key; it is the host's whole identity.
	AgentKey string `json:"agent_key"`

	// Environment tells the device agent what kind of host it is on.
	Environment string `json:"environment"`

	// HideUI suppresses the tray UI; nobody is at an agent's host to see it.
	HideUI bool `json:"hide_ui"`

	// AutoUpdate is "disabled" on a host that lives for one session and
	// "automatic" on one nobody is around to accept an update prompt on.
	AutoUpdate string `json:"auto_update"`

	// ControlPlaneURL overrides the control plane; omitted for production.
	ControlPlaneURL string `json:"_control_plane_url,omitempty"`
}

// deviceAgentInstallScript installs the device agent on a Linux host and
// enrolls it under an agent identity. The key is written to managed enrollment
// with owner-only permissions before it is ever on disk, and the script never
// prints it.
func deviceAgentInstallScript(controlPlane, key string, mode deviceAgentRunMode) (string, error) {
	config := deviceAgentManagedConfig{
		V:               deviceAgentManagedConfigVersion,
		AgentKey:        key,
		Environment:     "server",
		HideUI:          true,
		AutoUpdate:      "automatic",
		ControlPlaneURL: "",
	}
	var run string
	switch mode {
	case deviceAgentRunModeEphemeral:
		config.Environment = "ephemeral"
		config.AutoUpdate = "disabled"
		run = `  # 3) Reconcile once and exit. Rerun at the start of each session.
  "$BIN_DIR/speakeasyd" sync --once`
	case deviceAgentRunModeService:
		run = `  # 3) Register and start the background service under this account.
  # Keep the per-user service running after logout; root needs no linger.
  if [ -n "$SUDO" ]; then
    sudo loginctl enable-linger "$(id -un)"
  fi
  "$BIN_DIR/speakeasyd" -service install
  "$BIN_DIR/speakeasyd" -service start`
	default:
		return "", fmt.Errorf("unknown device agent run mode %q", mode)
	}
	if strings.TrimRight(controlPlane, "/") != deviceAgentDefaultControlPlane {
		config.ControlPlaneURL = controlPlane
	}
	// The JSON goes into a quoted heredoc, so nothing in it is expanded, and
	// json.Marshal escapes every control character, so no value can end the
	// heredoc early.
	managed, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode managed config: %w", err)
	}

	path := shellSingleQuote(deviceAgentManagedConfigPath)
	dir := shellSingleQuote(deviceAgentManagedConfigPath[:strings.LastIndex(deviceAgentManagedConfigPath, "/")])

	// Everything runs inside main, called on the last line: piped into sh, the
	// whole script is read before any of it runs, so a truncated download
	// runs nothing and no command can read the rest of the script as input.
	return `#!/bin/sh
# Installs the Gram device agent on this Linux host under an agent identity.
# Generated for one use. The key below is live: treat this file as a secret.
set -eu

main() {
  if [ "$(id -u)" = 0 ]; then
    SUDO=""; BIN_DIR=/usr/local/bin
  else
    SUDO="sudo"; BIN_DIR="$HOME/.local/bin"
  fi

  # 1) Install the device agent (latest stable, checksum-verified). Download
  #    over HTTPS only, and in full, before running it.
  INSTALLER="$(mktemp)"
  trap 'rm -f "$INSTALLER"' EXIT
  curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
    -o "$INSTALLER" ` + shellSingleQuote(deviceAgentInstallerURL) + `
  sh "$INSTALLER" --install-dir "$BIN_DIR"

  # 2) Agent identity. The key is this host's only credential.
  $SUDO mkdir -p ` + dir + `
  # Create it private first so the key is never briefly world-readable.
  $SUDO install -m 0600 /dev/null ` + path + `
  $SUDO tee ` + path + ` >/dev/null <<'JSON'
` + string(managed) + `
JSON
  # Readable only by root and the account that runs the agent.
  if [ -z "$SUDO" ]; then
    chmod 0600 ` + path + `
  else
    sudo chown root:"$(id -gn)" ` + path + `
    sudo chmod 0640 ` + path + `
    # Safe under a user-private group; otherwise the whole group can read it.
    if [ "$(id -gn)" != "$(id -un)" ]; then
      echo "warning: every member of group '$(id -gn)' can read the agent key in ` + deviceAgentManagedConfigPath + `" >&2
    fi
  fi

` + run + `

  echo "Device agent installed and enrolled."
}

main "$@"
`, nil
}
