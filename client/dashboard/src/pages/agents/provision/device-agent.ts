import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";

import {
  ANY_RESOURCE,
  canNarrowResource,
  requestNarrowsPolicy,
  type GrantSelection,
} from "../agent-api-key-grants";

/**
 * What an agent identity is provisioned for. An agent is one or the other,
 * never both: a device agent key is written to disk on a shared host, so it
 * must not double as a credential that reaches MCP servers.
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

/**
 * The purpose an existing agent was provisioned for, read from its stored
 * policy. An agent that can sync the device agent is a device agent; anything
 * else is provisioned for MCP, which is what every agent was before.
 */
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
 * Why this person cannot provision a device agent, or null when they can.
 * Organization scopes can only be delegated by an org admin, so offering the
 * option to anyone else is offering a flow that fails at the last step.
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

/** Why a key cannot be minted, naming org:admin only when an org scope is missing. */
export function undelegableScopesMessage(missingScopes: string[]): string {
  const scopes = missingScopes.join(", ");
  if (missingScopes.some((scope) => scope.startsWith("org:")))
    return `You cannot delegate ${scopes} to this agent. Organization scopes require org:admin, held by both you and the agent's owner.`;
  return `You cannot delegate ${scopes} to this agent. Check the agent's policy, and that you and its owner hold these permissions.`;
}

/** Where a reviewed script is saved; deleted once it has run. */
export const REVIEW_FILE = "gram-device-agent.sh";

/**
 * The review-first form of a one-line command: the same single-use URL saved
 * to a file instead of piped to a shell. The file carries the key, so it is
 * created owner-only (an existing file would keep its own mode, so it goes
 * first), and it is removed after the run whether or not the run succeeded,
 * in a subshell that keeps the script's exit status.
 */
export function reviewCommands(command: string): {
  fetch: string;
  run: string;
} {
  const url = command.replace(/^curl -fsSL /, "").replace(/ \| sh$/, "");
  return {
    fetch: `rm -f ${REVIEW_FILE} && (umask 077 && curl -fsSL ${url} -o ${REVIEW_FILE})`,
    run: `(sh ${REVIEW_FILE}; rc=$?; rm -f ${REVIEW_FILE}; exit $rc)`,
  };
}
