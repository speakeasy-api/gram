import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";

import {
  ANY_RESOURCE,
  canNarrowResource,
  requestNarrowsPolicy,
  type GrantSelection,
} from "../agent-api-key-grants";

/**
 * What an agent is provisioned for: one or the other, never both. A device
 * agent key is written to disk, so it must not also reach MCP servers.
 */
export type AgentPurpose = "mcp" | "device-agent";

/** How the device agent runs on the agent's host. */
export type DeviceAgentRunMode = "ephemeral" | "service";

const MCP_CONNECT_SCOPE = "mcp:connect";
const DEVICE_AGENT_SYNC_SCOPE = "org:device_agent_sync";
const HOOKS_INGEST_SCOPE = "org:hooks_ingest";

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

/** The scopes the summary rail lists for a device agent. */
export const DEVICE_AGENT_SCOPES = [
  DEVICE_AGENT_SYNC_SCOPE,
  HOOKS_INGEST_SCOPE,
  "project:read",
];

/** An agent that can sync the device agent is a device agent; any other is MCP. */
export function agentPurposeFromPolicy(
  grants: AgentPolicyGrant[],
): AgentPurpose {
  return grants.some((grant) => grant.scope === DEVICE_AGENT_SYNC_SCOPE)
    ? "device-agent"
    : "mcp";
}

/** Whether a stored policy already reaches MCP servers. */
export function policyConnectsToMCP(grants: AgentPolicyGrant[]): boolean {
  return grants.some((grant) => grant.scope === MCP_CONNECT_SCOPE);
}

/**
 * Why this person cannot provision a device agent, or null when they can. Only
 * an org admin can delegate its organization scopes.
 */
export function deviceAgentPurposeBlocked({
  deviceAgentEnabled,
  isOrgAdmin,
}: {
  deviceAgentEnabled: boolean;
  isOrgAdmin: boolean;
}): string | null {
  if (!deviceAgentEnabled)
    return "The device agent is not enabled for this organization.";
  if (!isOrgAdmin) return "Provisioning a device agent requires org:admin.";
  return null;
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
  // Strict containment: a candidate constraining any dimension the required
  // grant leaves open would mint a key too narrow for the device agent.
  return requestNarrowsPolicy(candidate.selector, required.selector);
}

/**
 * Picks a delegable candidate for each required grant, narrowed to the required
 * resource, and returns the scopes no candidate covers.
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
    // An issued `*` kind or id means every resource, so pin any wildcard the
    // requirement fixes. canNarrowResource is false for a wildcard kind, so
    // this cannot be left to narrowing.
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

/** Why a key cannot be minted, naming org:admin only when an org scope is missing. */
export function undelegableScopesMessage(missingScopes: string[]): string {
  const scopes = missingScopes.join(", ");
  if (missingScopes.some((scope) => scope.startsWith("org:")))
    return `You cannot delegate ${scopes} to this agent. Organization scopes require org:admin, held by both you and the agent's owner.`;
  return `You cannot delegate ${scopes} to this agent. Check the agent's policy, and that you and its owner hold these permissions.`;
}

/** The single-use script URL inside a one-line install command. */
const INSTALL_URL = /^https?:\/\/[^\s']+\/agent-mcp\/install\/[A-Za-z0-9_-]+$/;

/**
 * The one-line command as download-then-run, or null if it holds no install
 * URL. The script holds the key, so it goes to a fresh owner-only mktemp file
 * (no existing path is reused) and is removed after the run, keeping the exit
 * status. Both lines must run in the same shell, which holds `$f`.
 */
export function reviewCommands(
  command: string,
): { fetch: string; run: string } | null {
  const url = command.split(" ").find((token) => INSTALL_URL.test(token));
  if (!url) return null;
  return {
    fetch: `f=$(mktemp ./gram-device-agent.XXXXXX) && curl -fsSL '${url}' -o "$f" && echo "Saved to $f"`,
    run: `(sh "$f"; rc=$?; rm -f "$f"; exit $rc)`,
  };
}
