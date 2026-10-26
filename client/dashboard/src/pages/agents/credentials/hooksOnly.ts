import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { Agents } from "@gram/client/sdk/agents.js";
import {
  discoverUnscopedGrants,
  HOOKS_INGEST_SCOPE,
  hooksIngestPolicyGrant,
} from "../agent-hook-events";

/**
 * An agent identity that reaches no MCP server and only reports what its
 * coding agent does: the observability plugin sends hook events under it, and
 * the device agent syncs the plugins assigned to it.
 *
 * These are the two grants that takes, and nothing else. Both are satisfied
 * by `org:admin`, so an administrator can delegate them without a grant of
 * their own.
 */

/** Let the device agent sync the plugins assigned to this agent. */
export const DEVICE_AGENT_SYNC_SCOPE = "org:device_agent_sync";

const HOOKS_ONLY_SCOPES: readonly string[] = [
  HOOKS_INGEST_SCOPE,
  DEVICE_AGENT_SYNC_SCOPE,
];

/**
 * The policy a hooks-only agent is created with: the hook ingestion grant
 * every new agent gets, and plugin sync. Plugin sync takes no project bound:
 * the server rejects one on that scope.
 */
export function hooksOnlyPolicyGrants(
  organizationId: string,
  projectId: string,
): AgentPolicyGrantForm[] {
  return [
    hooksIngestPolicyGrant(organizationId, projectId),
    {
      effect: "allow",
      scope: DEVICE_AGENT_SYNC_SCOPE,
      selector: { resourceKind: "org", resourceId: organizationId },
    },
  ];
}

/**
 * The grants a hooks-only key may request, read from delegable grant
 * discovery rather than written by hand: the server answers with what the
 * agent, its owner and the caller all hold, so a key never asks for more.
 *
 * Unscoped, because these grants name the organization rather than a server.
 * Anything else discovery returns is left out: this key reaches no server.
 */
export async function discoverHooksOnlyGrants(
  agents: Pick<Agents, "listDelegableGrants">,
  agentId: string,
  signal: AbortSignal,
): Promise<AgentPolicyGrantForm[]> {
  const grants = await discoverUnscopedGrants(agents, agentId, signal);
  return grants.filter(
    (grant) =>
      grant.effect === "allow" && HOOKS_ONLY_SCOPES.includes(grant.scope),
  );
}

/** The scopes a hooks-only key needs that discovery did not return. */
export function missingHooksOnlyScopes(
  grants: AgentPolicyGrantForm[],
): string[] {
  return HOOKS_ONLY_SCOPES.filter(
    (scope) => !grants.some((grant) => grant.scope === scope),
  );
}
