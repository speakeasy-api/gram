// Builders for the Claude Code on the web (Anthropic-hosted VM) device-agent
// path. Kept pure so the install contract is unit-tested: the setup script
// lays down binaries, managed.json, and the per-session bootstrap; SessionStart
// only invokes that bootstrap. We never emit Claude observability hooks here.

export const RELEASES_BASE =
  "https://storage.googleapis.com/speakeasy-device-agent-releases-prod";

export const MANIFEST_URL = `${RELEASES_BASE}/releases.json`;

/** Sentinel spliced into the setup script until an org_token is minted. */
export const CLOUD_ORG_TOKEN_SENTINEL = "__SLOT_orgToken__";

/** Stable path provisioned by the setup script and invoked by SessionStart. */
export const CLOUD_AGENT_BOOTSTRAP_PATH = "/usr/local/bin/speakeasy-bootstrap";

const CLOUD_HELPER_PATH = "/usr/lib/speakeasy/speakeasy-helper";
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

// Same charset the OS-tile snippets require: a manifest version is inlined
// into a root shell script, so reject anything that isn't strict semver
// (optionally with a prerelease suffix).
export const PINNED_AGENT_VERSION =
  /^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/;

export type CloudSetupScriptInput = {
  version: string;
  orgSlug: string;
  orgName: string;
  orgToken: string;
  /** Identity used by agent.getPlugins and remote-session reporting. */
  email: string;
};

export function buildCloudManagedConfig(
  input: Omit<CloudSetupScriptInput, "version">,
): Record<string, unknown> {
  const email = input.email.trim();
  if (!EMAIL_PATTERN.test(email)) {
    throw new Error("a valid remote session identity email is required");
  }

  const config: Record<string, unknown> = {
    v: 1,
    email,
  };
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
 * Idempotent, cloud-only bootstrap installed into the cached filesystem.
 * It mirrors the device-agent ephemeral-VM contract: the helper runs as root,
 * the daemon runs as the same user as Claude, and startup waits for the first
 * policy sync without making a transient agent failure block Claude entirely.
 */
export function buildCloudAgentBootstrapScript(): string {
  return `#!/usr/bin/env bash
# Start Speakeasy enforcement for an Anthropic-hosted Claude Code session.
set -uo pipefail

if [ "\${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

LOG_DIR="\${SPEAKEASY_LOG_DIR:-\${TMPDIR:-/tmp}}"
WAIT_SECS="\${SPEAKEASY_WAIT_SECS:-30}"
HELPER="${CLOUD_HELPER_PATH}"
DAEMON="/usr/local/bin/speakeasyd"
CLI="/usr/local/bin/speakeasy"

log() { printf 'speakeasy-bootstrap: %s\\n' "$*" >&2; }

EXPECT_ROOT=0
if [ -S /run/com.speakeasy.helper.sock ] && pgrep -f '(^|/)speakeasy-helper($| )' >/dev/null 2>&1; then
  EXPECT_ROOT=1
else
  if [ ! -x "$HELPER" ]; then
    log "helper not found at $HELPER; managed enforcement will fall back to the user layer"
  elif [ "$(id -u)" = "0" ]; then
    "$HELPER" >>"$LOG_DIR/speakeasy-helper.log" 2>&1 &
    EXPECT_ROOT=1
  elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    sudo -n "$HELPER" >>"$LOG_DIR/speakeasy-helper.log" 2>&1 &
    EXPECT_ROOT=1
  else
    log "cannot start the root helper; managed enforcement will fall back to the user layer"
  fi
fi

if ! pgrep -u "$(id -u)" -x speakeasyd >/dev/null 2>&1; then
  "$DAEMON" >>"$LOG_DIR/speakeasyd.log" 2>&1 &
fi

deadline=$((SECONDS + WAIT_SECS))
last_sync_nudge=-5
while [ "$SECONDS" -lt "$deadline" ]; do
  if status=$("$CLI" status 2>/dev/null) && printf '%s' "$status" | grep -q '^synced:'; then
    if [ "$EXPECT_ROOT" = "1" ] && printf '%s' "$status" | grep -q 'pending:'; then
      if [ -S /run/com.speakeasy.helper.sock ] && [ $((SECONDS - last_sync_nudge)) -ge 5 ]; then
        last_sync_nudge=$SECONDS
        "$CLI" sync >/dev/null 2>&1 || true
      fi
      sleep 0.5
      continue
    fi
    log "policy synced"
    exit 0
  fi
  sleep 0.5
done

log "policy did not sync within \${WAIT_SECS}s; check $LOG_DIR/speakeasyd.log"
"$CLI" status 2>&1 || true
exit 0
`;
}

/**
 * Anthropic environment setup script (runs as root, cache miss only).
 * Installs linux_amd64 binaries + the helper .deb, writes managed.json, and
 * installs the bootstrap that SessionStart invokes. It does not start the
 * agent and does not write Claude settings.
 */
export function buildCloudSetupScript(input: CloudSetupScriptInput): string {
  const version = assertPinnedAgentVersion(input.version);
  const managedJson = JSON.stringify(buildCloudManagedConfig(input), null, 2);
  const bootstrapScript = buildCloudAgentBootstrapScript();
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

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
curl -fsSL -o checksums.txt "$BASE/checksums.txt"
fetch_and_verify() {
  curl -fSL -o "$1" "$BASE/$1"
  grep " $1$" checksums.txt | sha256sum -c -
}

install -d -m 0755 /usr/local/bin
fetch_and_verify "${daemon}"
install -m 0755 "${daemon}" /usr/local/bin/speakeasyd
fetch_and_verify "${cli}"
install -m 0755 "${cli}" /usr/local/bin/speakeasy
fetch_and_verify "${helperPkg}"
DEBIAN_FRONTEND=noninteractive apt-get install -y "./${helperPkg}"

cat >${CLOUD_AGENT_BOOTSTRAP_PATH} <<'SPEAKEASY_BOOTSTRAP'
${bootstrapScript}SPEAKEASY_BOOTSTRAP
chmod 0755 ${CLOUD_AGENT_BOOTSTRAP_PATH}

install -d -m 0755 /etc/speakeasy
cat >/etc/speakeasy/managed.json <<'SPEAKEASY_MANAGED_JSON'
${managedJson}
SPEAKEASY_MANAGED_JSON
# The daemon runs as Claude's session user and must be able to read this file.
chmod 0644 /etc/speakeasy/managed.json
`;
}

/**
 * SessionStart invokes the bootstrap installed by the environment setup
 * script. Process management stays in that script instead of being embedded
 * as a multiline JSON command.
 */
export function buildCloudAgentStartCommand(): string {
  return CLOUD_AGENT_BOOTSTRAP_PATH;
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
            matcher: "startup|resume",
            hooks: [
              {
                type: "command",
                command: buildCloudAgentStartCommand(),
                timeout: 45,
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
