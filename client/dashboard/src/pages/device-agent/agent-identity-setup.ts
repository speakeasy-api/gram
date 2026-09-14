import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { Key } from "@gram/client/models/components/key.js";

import {
  ANY_RESOURCE,
  canNarrowResource,
  requestNarrowsPolicy,
  type GrantSelection,
} from "@/pages/agents/agent-api-key-grants";

import { RELEASES_BASE } from "./cloud-setup";

const INSTALL_SCRIPT_URL = `${RELEASES_BASE}/install.sh`;
const PRODUCTION_SERVER_URL = "https://app.getgram.ai";

const DEVICE_AGENT_SYNC_SCOPE = "org:device_agent_sync";
const HOOKS_INGEST_SCOPE = "org:hooks_ingest";

export type AgentHostOS = "linux" | "macos";
export type AgentRunMode = "ephemeral" | "service";

const MANAGED_CONFIG_PATH: Record<AgentHostOS, string> = {
  linux: "/etc/speakeasy/managed.json",
  macos: "/Library/Application Support/Speakeasy/managed.json",
};

// Printable ASCII minus quote, backslash, and space: the key lands verbatim in
// JSON written through a quoted heredoc.
const JSON_SAFE_VALUE = /^[\x21\x23-\x5b\x5d-\x7e]+$/;

/** The direct policy grants an agent needs to run the device agent. */
export function deviceAgentPolicyGrants(
  projectId: string,
): AgentPolicyGrantForm[] {
  return [
    {
      effect: "allow",
      scope: DEVICE_AGENT_SYNC_SCOPE,
      selector: { resourceKind: "org", resourceId: ANY_RESOURCE },
    },
    {
      effect: "allow",
      scope: HOOKS_INGEST_SCOPE,
      selector: { resourceKind: "org", resourceId: ANY_RESOURCE },
    },
    {
      effect: "allow",
      scope: "project:read",
      selector: { resourceKind: "project", resourceId: projectId },
    },
  ];
}

/** The required grants the stored policy does not already cover. */
export function missingPolicyGrants(
  stored: AgentPolicyGrant[],
  required: AgentPolicyGrantForm[],
): AgentPolicyGrantForm[] {
  return required.filter(
    (form) =>
      !stored.some(
        (grant) =>
          grant.scope === form.scope &&
          requestNarrowsPolicy(grant.selector, form.selector),
      ),
  );
}

function candidateCovers(
  candidate: AgentPolicyGrantForm,
  required: AgentPolicyGrantForm,
): boolean {
  if (candidate.effect !== "allow" || candidate.scope !== required.scope)
    return false;
  if (candidate.selector.resourceKind !== required.selector.resourceKind)
    return false;
  const id = candidate.selector.resourceId;
  return id === ANY_RESOURCE || id === required.selector.resourceId;
}

/**
 * Picks the delegable candidate for each required grant, narrowed to the
 * required resource. Returns the scopes no candidate covers, since those mean
 * the owner or the caller cannot delegate them.
 */
export function selectDeviceAgentKeyGrants(
  delegable: AgentPolicyGrantForm[],
  required: AgentPolicyGrantForm[],
): { selections: GrantSelection[]; missingScopes: string[] } {
  const selections: GrantSelection[] = [];
  const missingScopes: string[] = [];
  for (const form of required) {
    const candidates = delegable.filter((c) => candidateCovers(c, form));
    const grant =
      candidates.find(
        (c) => c.selector.resourceId === form.selector.resourceId,
      ) ?? candidates[0];
    if (!grant) {
      missingScopes.push(form.scope);
      continue;
    }
    const narrowResource =
      canNarrowResource(grant) &&
      form.selector.resourceId !== ANY_RESOURCE &&
      grant.selector.resourceId !== form.selector.resourceId;
    selections.push({
      grant,
      narrowing: narrowResource ? { resourceId: form.selector.resourceId } : {},
    });
  }
  return { selections, missingScopes };
}

/** The most recent use of any of the agent's keys: the device agent's check-in. */
export function lastSeenKey(keys: Key[]): Key | null {
  let latest: Key | null = null;
  for (const key of keys) {
    if (!key.lastAccessedAt) continue;
    if (!latest?.lastAccessedAt || key.lastAccessedAt > latest.lastAccessedAt)
      latest = key;
  }
  return latest;
}

export type AgentIdentitySnippetInput = {
  agentKey: string;
  os: AgentHostOS;
  mode: AgentRunMode;
  serverURL: string;
};

export function managedConfigPath(os: AgentHostOS): string {
  return MANAGED_CONFIG_PATH[os];
}

export function buildAgentIdentityManagedConfig(
  input: AgentIdentitySnippetInput,
): string {
  if (!input.agentKey || !JSON_SAFE_VALUE.test(input.agentKey)) {
    throw new Error("a valid agent key is required");
  }
  const serverURL = input.serverURL.replace(/\/+$/, "");
  const config: Record<string, string | number | boolean> = {
    v: 1,
    agent_key: input.agentKey,
    environment: input.mode === "ephemeral" ? "ephemeral" : "server",
    hide_ui: true,
  };
  // A short-lived box has no use for updates; an unattended host has nobody
  // to accept a notify prompt.
  config.auto_update = input.mode === "ephemeral" ? "disabled" : "automatic";
  if (serverURL !== PRODUCTION_SERVER_URL)
    config._control_plane_url = serverURL;
  return JSON.stringify(config, null, 2);
}

/**
 * Install, configure, and start the device agent under an agent identity. No
 * email, version, or checksum: the hosted script resolves the latest stable
 * release and verifies it, and the key is the whole identity.
 */
export function buildAgentIdentitySnippet(
  input: AgentIdentitySnippetInput,
): string {
  const config = buildAgentIdentityManagedConfig(input);
  const path = managedConfigPath(input.os);
  const dir = path.slice(0, path.lastIndexOf("/"));
  const linger =
    input.os === "linux"
      ? `# Keep the per-user service running after logout.
loginctl enable-linger "$USER"
`
      : "";
  const run =
    input.mode === "ephemeral"
      ? `# 3) Reconcile once and exit. Rerun at the start of each session.
"$BIN_DIR/speakeasyd" sync --once`
      : `# 3) Register and start the background service. Run as the account
#    the agent should manage, not as root.
${linger}"$BIN_DIR/speakeasyd" -service install
"$BIN_DIR/speakeasyd" -service start`;
  return `#!/usr/bin/env sh
set -eu
if [ "$(id -u)" = 0 ]; then
  SUDO=""; BIN_DIR=/usr/local/bin
else
  SUDO="sudo"; BIN_DIR="$HOME/.local/bin"
fi

# 1) Install the device agent (latest stable, checksum-verified).
curl -fsSL ${INSTALL_SCRIPT_URL} | sh -s -- --install-dir "$BIN_DIR"

# 2) Agent identity. The key is this machine's only credential.
$SUDO mkdir -p '${dir}'
$SUDO tee '${path}' >/dev/null <<'JSON'
${config}
JSON
$SUDO chmod 0644 '${path}'

${run}
`;
}
