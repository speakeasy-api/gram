// Builders for the Claude Code on the web (Anthropic-hosted VM) device-agent
// path. Kept pure so the install contract is unit-tested: the setup script
// only lays down binaries + helper + managed.json; SessionStart only starts
// the agent. We never emit Claude observability hook commands here.

export const RELEASES_BASE =
  "https://storage.googleapis.com/speakeasy-device-agent-releases-prod";

export const MANIFEST_URL = `${RELEASES_BASE}/releases.json`;

/** Sentinel spliced into the setup script until an org_token is minted. */
export const CLOUD_ORG_TOKEN_SENTINEL = "__SLOT_orgToken__";

/** Anchor for the Cloud sessions section and the onboarding alert jump-link. */
export const CLOUD_SESSIONS_ANCHOR = "cloud-sessions";

// Same charset the OS-tile snippets require: a manifest version is inlined
// into a root shell script, so reject anything that isn't strict semver
// (optionally with a prerelease suffix).
const PINNED_AGENT_VERSION = /^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/;

export type CloudSetupScriptInput = {
  version: string;
  orgSlug: string;
  orgName: string;
  orgToken: string;
  /** Required for getPlugins. Omit for degraded mode (daemon runs, syncs nothing). */
  email?: string;
};

export function buildCloudManagedConfig(
  input: Omit<CloudSetupScriptInput, "version">,
): Record<string, unknown> {
  const config: Record<string, unknown> = {
    v: 1,
  };
  const email = input.email?.trim();
  if (email) {
    config.email = email;
  }
  config.org_token = input.orgToken;
  config.org_slug = input.orgSlug;
  config.org_name = input.orgName;
  config.auto_update = "disabled";
  config.hide_ui = true;
  return config;
}

function assertPinnedAgentVersion(version: string): string {
  if (!PINNED_AGENT_VERSION.test(version)) {
    throw new Error(
      `refusing to interpolate agent version into a root shell script: ${version}`,
    );
  }
  return version;
}

/**
 * Anthropic environment setup script (runs as root, cache miss only).
 * Installs linux_amd64 binaries + the helper .deb and writes managed.json.
 * Does not start the agent and does not write Claude settings.
 */
export function buildCloudSetupScript(input: CloudSetupScriptInput): string {
  const version = assertPinnedAgentVersion(input.version);
  const managedJson = JSON.stringify(buildCloudManagedConfig(input), null, 2);
  const base = `${RELEASES_BASE}/v${version}`;
  const daemon = `speakeasyd_${version}_linux_amd64`;
  const cli = `speakeasy_${version}_linux_amd64`;
  const helperPkg = `speakeasy-helper_${version}_linux_amd64.deb`;

  return `#!/usr/bin/env bash
# Speakeasy device agent — Anthropic-hosted Claude Code environment.
# Runs as root on a cache miss only. Does not start the agent: Anthropic
# snapshots the filesystem and skips this script on later sessions, so
# SessionStart is what brings the daemon up.
set -euo pipefail

VERSION="${version}"
BASE="${base}"

install -d -m 0755 /usr/local/bin
curl -fSL -o /usr/local/bin/speakeasyd "$BASE/${daemon}"
curl -fSL -o /usr/local/bin/speakeasy "$BASE/${cli}"
chmod 0755 /usr/local/bin/speakeasyd /usr/local/bin/speakeasy

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
PKG="${helperPkg}"
curl -fSL -o "$PKG" "$BASE/$PKG"
curl -fsSL "$BASE/checksums.txt" | grep " $PKG$" | sha256sum -c -
DEBIAN_FRONTEND=noninteractive apt-get install -y "./$PKG"

install -d -m 0755 /etc/speakeasy
cat >/etc/speakeasy/managed.json <<'SPEAKEASY_MANAGED_JSON'
${managedJson}
SPEAKEASY_MANAGED_JSON
chmod 0644 /etc/speakeasy/managed.json
`;
}

/**
 * Idempotent start for a VM with no service manager. Mirrors the measured
 * ephemeral-vm bootstrap: start speakeasyd as the current user, and the
 * helper as root when possible. Gated so pasting into laptop Managed
 * Settings is a no-op outside Claude Code on the web.
 */
export function buildCloudAgentStartCommand(): string {
  return [
    '[ "${CLAUDE_CODE_REMOTE:-}" = "true" ] || exit 0',
    'if ! pgrep -u "$(id -u)" -x speakeasyd >/dev/null 2>&1; then',
    '  speakeasyd >>"${TMPDIR:-/tmp}/speakeasyd.log" 2>&1 &',
    "fi",
    "if ! { [ -S /run/com.speakeasy.helper.sock ] && pgrep -f '(^|/)speakeasy-helper($| )' >/dev/null 2>&1; }; then",
    '  if [ "$(id -u)" = "0" ]; then',
    '    speakeasy-helper >>"${TMPDIR:-/tmp}/speakeasy-helper.log" 2>&1 &',
    "  elif command -v sudo >/dev/null 2>&1; then",
    '    sudo -n speakeasy-helper >>"${TMPDIR:-/tmp}/speakeasy-helper.log" 2>&1 &',
    "  fi",
    "fi",
  ].join("\n");
}

/**
 * SessionStart snippet to merge into org Managed Settings or repo
 * `.claude/settings.json`. Starts the agent only — not observability policy.
 */
export function buildCloudAgentStartHook(): string {
  return `${JSON.stringify(
    {
      hooks: {
        SessionStart: [
          {
            hooks: [
              {
                type: "command",
                command: buildCloudAgentStartCommand(),
                timeout: 30,
              },
            ],
          },
        ],
      },
    },
    null,
    2,
  )}\n`;
}

/** Placeholder merge snippet; the env id is copied from Claude after create. */
export function buildCloudDefaultEnvironmentSnippet(): string {
  return `${JSON.stringify(
    {
      remote: { defaultEnvironmentId: "env_…" },
    },
    null,
    2,
  )}\n`;
}
