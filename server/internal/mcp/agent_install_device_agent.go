package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// deviceAgentRunMode is how the device agent runs on an agent's host.
type deviceAgentRunMode string

const (
	// deviceAgentRunModeEphemeral syncs once and exits, for short-lived hosts.
	deviceAgentRunModeEphemeral deviceAgentRunMode = "ephemeral"

	// deviceAgentRunModeService installs a per-user background service.
	deviceAgentRunModeService deviceAgentRunMode = "service"
)

// deviceAgentRunModes lists the modes an install code may carry.
var deviceAgentRunModes = []deviceAgentRunMode{deviceAgentRunModeEphemeral, deviceAgentRunModeService}

const (
	// deviceAgentInstallerURL resolves and verifies the latest stable release,
	// so the script pins no version or checksum.
	deviceAgentInstallerURL = "https://storage.googleapis.com/speakeasy-device-agent-releases-prod/install.sh"

	// deviceAgentManagedConfigPath is the device agent's managed enrollment
	// file on Linux. macOS hosts use the signed package through MDM.
	deviceAgentManagedConfigPath = "/etc/speakeasy/managed.json"

	// deviceAgentDefaultControlPlane is used when enrollment names none.
	deviceAgentDefaultControlPlane = "https://app.getgram.ai"

	// deviceAgentManagedConfigVersion is the managed enrollment schema version.
	deviceAgentManagedConfigVersion = 1
)

// deviceAgentManagedConfig is the device agent's managed enrollment file.
type deviceAgentManagedConfig struct {
	// V is the schema version.
	V int `json:"v"`

	// AgentKey is the agent API key, the host's only credential.
	AgentKey string `json:"agent_key"`

	// Environment tells the device agent what kind of host it is on.
	Environment string `json:"environment"`

	// HideUI suppresses the tray UI on an unattended host.
	HideUI bool `json:"hide_ui"`

	// AutoUpdate is "disabled" for ephemeral hosts and "automatic" otherwise.
	AutoUpdate string `json:"auto_update"`

	// ControlPlaneURL overrides the control plane; omitted for production.
	ControlPlaneURL string `json:"_control_plane_url,omitempty"`
}

// deviceAgentInstallScript installs and enrolls the device agent on a Linux
// host. The key file is readable only by the account that runs the agent, and
// a failed install removes it.
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
		run = `  # 3) Sync once and exit, then say how to sync again.
  "$BIN_DIR/speakeasyd" sync --once
  echo "To sync again, run: $BIN_DIR/speakeasyd sync --once"`
	case deviceAgentRunModeService:
		run = `  # 3) Install and start the background service for this account, and
  #    keep it running after logout.
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
	// The heredoc is quoted and json.Marshal escapes control characters, so no
	// value can be expanded or end the heredoc early.
	managed, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode managed config: %w", err)
	}

	path := shellSingleQuote(deviceAgentManagedConfigPath)
	dir := shellSingleQuote(deviceAgentManagedConfigPath[:strings.LastIndex(deviceAgentManagedConfigPath, "/")])

	// Wrapping the body in main means a truncated download runs nothing when
	// piped to sh.
	return `#!/bin/sh
# Installs the Gram device agent on this Linux host under an agent identity.
# Generated for one use. The key below is live: treat this file as a secret.
set -eu

MANAGED=` + path + `

main() {
  if [ "$(id -u)" = 0 ]; then
    SUDO=""; BIN_DIR=/usr/local/bin
  else
    SUDO="sudo"; BIN_DIR="$HOME/.local/bin"
  fi

  # 1) Install the latest device agent over HTTPS, downloading it in full
  #    before running it.
  INSTALLER="$(mktemp)"
  KEY_WRITTEN=""
  trap 'status=$?; rm -f "$INSTALLER"; if [ "$status" -ne 0 ] && [ -n "$KEY_WRITTEN" ]; then $SUDO rm -f "$MANAGED"; echo "Install failed; removed the agent key." >&2; fi' EXIT
  curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
    -o "$INSTALLER" ` + shellSingleQuote(deviceAgentInstallerURL) + `
  sh "$INSTALLER" --install-dir "$BIN_DIR"

  # 2) Write the agent key.
  $SUDO mkdir -p ` + dir + `
  # Owner-only before the key is written, so it is never briefly readable.
  KEY_WRITTEN=1
  $SUDO install -m 0600 -o "$(id -un)" /dev/null "$MANAGED"
  $SUDO tee "$MANAGED" >/dev/null <<'JSON'
` + string(managed) + `
JSON

` + run + `

  echo "Device agent installed and enrolled."
}

main "$@"
`, nil
}
