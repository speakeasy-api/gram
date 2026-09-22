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

/** Identifies the key issuance mutation, so selection can lock while it runs. */
export const ISSUE_AGENT_KEY_MUTATION = ["agent-identity", "issue-key"];

/** Agent identity hosts are Linux; macOS hosts use the signed .pkg via MDM. */
export const MANAGED_CONFIG_PATH = "/etc/speakeasy/managed.json";

export type AgentRunMode = "ephemeral" | "service";

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
          // A deny grant covers nothing: minting requires an allow candidate,
          // so counting one here would report access the next step refuses.
          grant.effect === "allow" &&
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
  // Strict containment: a candidate constraining any dimension the required
  // grant leaves open would mint a key too narrow for the device agent.
  return requestNarrowsPolicy(candidate.selector, required.selector);
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
    // A candidate may wildcard a dimension the requirement pins, and the
    // server reads a wildcard in an issued grant as "every resource" — so a
    // `*` kind or id would mint a key far broader than the chosen project.
    // canNarrowResource cannot close this: it is false for a wildcard kind
    // (no inventory names those resources), so specialize the candidate here
    // and let expandRequestedGrants clone an already-specific selector.
    const specialized: AgentPolicyGrantForm = {
      ...grant,
      selector: {
        ...grant.selector,
        ...(grant.selector.resourceKind === ANY_RESOURCE &&
        form.selector.resourceKind !== ANY_RESOURCE
          ? { resourceKind: form.selector.resourceKind }
          : {}),
        ...(grant.selector.resourceId === ANY_RESOURCE &&
        form.selector.resourceId !== ANY_RESOURCE
          ? { resourceId: form.selector.resourceId }
          : {}),
      },
    };
    const narrowResource =
      canNarrowResource(specialized) &&
      form.selector.resourceId !== ANY_RESOURCE &&
      specialized.selector.resourceId !== form.selector.resourceId;
    selections.push({
      grant: specialized,
      narrowing: narrowResource ? { resourceId: form.selector.resourceId } : {},
    });
  }
  return { selections, missingScopes };
}

/** The issued key, only while it still matches the chosen agent and project. */
export function issuedKeyFor<T extends { agentId: string; projectId: string }>(
  issued: T | null,
  agentId: string | undefined,
  projectId: string,
): T | null {
  return issued && issued.agentId === agentId && issued.projectId === projectId
    ? issued
    : null;
}

/** Why a key cannot be minted, naming org:admin only when an org scope is missing. */
export function undelegableScopesMessage(missingScopes: string[]): string {
  const scopes = missingScopes.join(", ");
  if (missingScopes.some((scope) => scope.startsWith("org:")))
    return `You cannot delegate ${scopes} to this agent. Organization scopes require org:admin, held by both you and the agent's owner.`;
  return `You cannot delegate ${scopes} to this agent. Check the agent's policy, and that you and its owner hold these permissions.`;
}

/**
 * Why the agent key must not be sent to this control plane, or null when it
 * may. The key travels on every request, so anything but HTTPS would expose it.
 */
export function controlPlaneURLError(serverURL: string): string | null {
  let url: URL;
  try {
    url = new URL(serverURL);
  } catch {
    return "The control-plane URL is invalid, so no install snippet can be generated.";
  }
  // No loopback exception: the snippet runs on the agent host, where
  // localhost is that machine, not the control plane.
  if (url.protocol === "https:") return null;
  return `The control plane at ${url.origin} is not served over HTTPS, so the agent key would travel in plaintext. No install snippet can be generated.`;
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
  mode: AgentRunMode;
  serverURL: string;
};

export function buildAgentIdentityManagedConfig(
  input: AgentIdentitySnippetInput,
): string {
  if (!input.agentKey || !JSON_SAFE_VALUE.test(input.agentKey)) {
    throw new Error("a valid agent key is required");
  }
  const urlError = controlPlaneURLError(input.serverURL);
  if (urlError) throw new Error(urlError);
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
 * Install, configure, and start the device agent on a Linux host under an
 * agent identity. No email, version, or checksum: the hosted script resolves
 * the latest stable release and verifies it, and the key is the whole identity.
 */
export function buildAgentIdentitySnippet(
  input: AgentIdentitySnippetInput,
): string {
  const config = buildAgentIdentityManagedConfig(input);
  const path = MANAGED_CONFIG_PATH;
  const dir = path.slice(0, path.lastIndexOf("/"));
  const run =
    input.mode === "ephemeral"
      ? `# 3) Reconcile once and exit. Rerun at the start of each session.
"$BIN_DIR/speakeasyd" sync --once`
      : `# 3) Register and start the background service. Run as the account
#    the agent should manage, not as root.
# Keep the per-user service running after logout; root needs no linger.
if [ -n "$SUDO" ]; then
  sudo loginctl enable-linger "$(id -un)"
fi
"$BIN_DIR/speakeasyd" -service install
"$BIN_DIR/speakeasyd" -service start`;
  return `#!/usr/bin/env sh
set -eu
if [ "$(id -u)" = 0 ]; then
  SUDO=""; BIN_DIR=/usr/local/bin
else
  SUDO="sudo"; BIN_DIR="$HOME/.local/bin"
fi

# 1) Install the device agent (latest stable, checksum-verified). Download
#    over HTTPS only, and first, so a failed or truncated fetch never runs.
INSTALLER="$(mktemp)"
trap 'rm -f "$INSTALLER"' EXIT
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \\
  -o "$INSTALLER" ${INSTALL_SCRIPT_URL}
sh "$INSTALLER" --install-dir "$BIN_DIR"

# 2) Agent identity. The key is this machine's only credential.
$SUDO mkdir -p '${dir}'
# Create it private first so the key is never briefly world-readable.
$SUDO install -m 0600 /dev/null '${path}'
$SUDO tee '${path}' >/dev/null <<'JSON'
${config}
JSON
# Readable only by root and the account that runs the agent.
if [ -z "$SUDO" ]; then
  chmod 0600 '${path}'
else
  sudo chown root:"$(id -gn)" '${path}'
  sudo chmod 0640 '${path}'
  # Safe under a user-private group; otherwise the whole group can read it.
  if [ "$(id -gn)" != "$(id -un)" ]; then
    echo "warning: every member of group '$(id -gn)' can read the agent key in ${path}" >&2
  fi
fi

${run}
`;
}
