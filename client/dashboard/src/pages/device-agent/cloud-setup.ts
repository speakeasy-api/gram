export const RELEASES_BASE =
  "https://storage.googleapis.com/speakeasy-device-agent-releases-prod";

export const MANIFEST_URL = `${RELEASES_BASE}/releases.json`;

/** Sentinel spliced into the startup script until an org_token is minted. */
export const CLOUD_ORG_TOKEN_SENTINEL = "__SLOT_orgToken__";

export const PINNED_AGENT_VERSION =
  /^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/;

export const RELEASE_SHA256 = /^[0-9a-f]{64}$/;

// Email and token land inside a JSON heredoc the script expands, so beyond
// shell quoting they must not carry JSON-breaking characters (quotes,
// backslashes, whitespace, or control characters).
// eslint-disable-next-line no-control-regex
const JSON_SAFE_VALUE = /^[^"\\\s\x00-\x1f]+$/;

export type CloudSetupCommandInput = {
  version: string;
  sha256: string;
  email: string;
  orgToken: string;
};

/**
 * Renders the Anthropic environment's setup script. It runs once as root
 * before the filesystem snapshot and does exactly three things: install the
 * pinned daemon, write managed enrollment, and register a SessionStart hook
 * that revives the daemon in each session. Everything after daemon start is
 * the agent's normal policy-sync loop — plugin install, tool config, and
 * enforcement all ride the same infrastructure as any other machine.
 */
export function buildCloudSetupCommand(input: CloudSetupCommandInput): string {
  const email = input.email.trim();
  if (
    !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) ||
    !JSON_SAFE_VALUE.test(email)
  ) {
    throw new Error("a valid remote session identity email is required");
  }
  if (!input.orgToken || !JSON_SAFE_VALUE.test(input.orgToken)) {
    throw new Error("a valid org token is required");
  }
  if (!PINNED_AGENT_VERSION.test(input.version)) {
    throw new Error("a valid pinned device agent version is required");
  }
  if (!RELEASE_SHA256.test(input.sha256)) {
    throw new Error("a valid device agent daemon checksum is required");
  }

  return `#!/usr/bin/env bash
set -euo pipefail

# Values from the Gram dashboard.
VERSION=${shellQuote(input.version)}
SHA256=${shellQuote(input.sha256)}
EMAIL=${shellQuote(email)}
ORG_TOKEN=${shellQuote(input.orgToken)}

# 1) Install the device agent daemon (pinned, checksum-verified).
curl -fsSL -o /usr/local/bin/speakeasyd \\
  "${RELEASES_BASE}/v\${VERSION}/speakeasyd_\${VERSION}_linux_amd64"
printf '%s  /usr/local/bin/speakeasyd\\n' "$SHA256" | sha256sum -c -
chmod 0755 /usr/local/bin/speakeasyd

# 2) Managed enrollment: the identity the daemon reads at startup, and the
#    same file that answers daemonless identity lookups from hooks. Everything
#    in this VM runs as root, so keep the org token root-only.
install -d -m 0755 /etc/speakeasy
cat > /etc/speakeasy/managed.json <<JSON
{
  "v": 1,
  "email": "\${EMAIL}",
  "org_token": "\${ORG_TOKEN}",
  "auto_update": "disabled",
  "hide_ui": true
}
JSON
chmod 0600 /etc/speakeasy/managed.json

# 3) SessionStart hook: starts the daemon in each session (Anthropic
#    snapshots files, not processes). flock holds the lock for the daemon's
#    lifetime, so a startup/resume double-fire can never double-start it.
install -d /root/.local/state/speakeasy/logs
install -d /root/.claude
cat > /root/.claude/settings.json <<'JSON'
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume",
        "hooks": [
          {
            "type": "command",
            "command": "setsid flock -n /run/speakeasyd.lock /usr/local/bin/speakeasyd >>/root/.local/state/speakeasy/logs/speakeasyd.log 2>&1 &",
            "async": true
          }
        ]
      }
    ]
  }
}
JSON
`;
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'\\''`)}'`;
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
