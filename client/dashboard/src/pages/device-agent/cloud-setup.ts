export const RELEASES_BASE =
  "https://storage.googleapis.com/speakeasy-device-agent-releases-prod";

export const MANIFEST_URL = `${RELEASES_BASE}/releases.json`;

/** Sentinel spliced into the startup script until an org_token is minted. */
export const CLOUD_ORG_TOKEN_SENTINEL = "__SLOT_orgToken__";

export const PINNED_AGENT_VERSION =
  /^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/;

export const RELEASE_SHA256 = /^[0-9a-f]{64}$/;

export type CloudSetupCommandInput = {
  version: string;
  sha256: string;
  email: string;
  orgToken: string;
};

export function buildCloudSetupCommand(input: CloudSetupCommandInput): string {
  const email = input.email.trim();
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    throw new Error("a valid remote session identity email is required");
  }
  if (!PINNED_AGENT_VERSION.test(input.version)) {
    throw new Error("a valid pinned device agent version is required");
  }
  if (!RELEASE_SHA256.test(input.sha256)) {
    throw new Error("a valid device agent CLI checksum is required");
  }

  const cliURL = `${RELEASES_BASE}/v${input.version}/speakeasy_${input.version}_linux_amd64`;

  return `#!/usr/bin/env bash
set -euo pipefail

CLI="$(mktemp)"
trap 'rm -f "$CLI"' EXIT

curl -fsSL -o "$CLI" ${shellQuote(cliURL)}
printf '%s  %s\\n' ${shellQuote(input.sha256)} "$CLI" | sha256sum -c -
chmod 0755 "$CLI"

SPEAKEASY_EMAIL=${shellQuote(email)} \\
SPEAKEASY_ORG_TOKEN=${shellQuote(input.orgToken)} \\
"$CLI" setup --anthropic-cloud
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
