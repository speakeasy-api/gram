import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { Agents } from "@gram/client/sdk/agents.js";
import { ANY_RESOURCE, delegableGrantKey } from "./agent-api-key-grants";

/**
 * Hook events are what the observability plugin reports about the coding
 * agent an agent identity runs under. An agent identity sends them by
 * default: it is created with the grant for them, bounded to its own
 * project, and its first key requests that grant.
 */

/** Send AI-tool hook events and telemetry. Checked per project. */
export const HOOKS_INGEST_SCOPE = "org:hooks_ingest";

/**
 * The hook ingestion grant a new agent is created with. Bounded to the
 * agent's own project, because every hook request names the project it is
 * sent to and an agent belongs to one.
 */
export function hooksIngestPolicyGrant(
  organizationId: string,
  projectId: string,
): AgentPolicyGrantForm {
  return {
    effect: "allow",
    scope: HOOKS_INGEST_SCOPE,
    selector: { resourceKind: "org", resourceId: organizationId, projectId },
  };
}

/**
 * Delegable grant discovery that names no server. Organization grants such as
 * hook ingestion name no server either, so only this form returns them. The
 * server answers with what the agent, its owner and the caller all hold.
 */
export async function discoverUnscopedGrants(
  agents: Pick<Agents, "listDelegableGrants">,
  agentId: string,
  signal: AbortSignal,
): Promise<AgentPolicyGrantForm[]> {
  const grants = await agents.listDelegableGrants({ agentId }, undefined, {
    signal,
  });
  if (!Array.isArray(grants)) {
    throw new Error("Invalid delegable permission response");
  }
  return grants;
}

/**
 * The hook ingestion grant a key for an agent in `projectId` may request,
 * taken from what discovery returned. A candidate bound to another project
 * is left out. One with no project bound is narrowed to this one, which a key
 * may always do, so a key never sends events for a project its agent does not
 * belong to.
 */
export function hooksIngestKeyGrants(
  delegable: AgentPolicyGrantForm[],
  projectId: string,
): AgentPolicyGrantForm[] {
  const grants = new Map<string, AgentPolicyGrantForm>();
  for (const grant of delegable) {
    if (grant.effect !== "allow" || grant.scope !== HOOKS_INGEST_SCOPE) {
      continue;
    }
    const bound = grant.selector.projectId;
    if (bound !== undefined && bound !== ANY_RESOURCE && bound !== projectId) {
      continue;
    }
    const narrowed: AgentPolicyGrantForm = {
      effect: grant.effect,
      scope: grant.scope,
      selector: { ...grant.selector, projectId },
    };
    grants.set(delegableGrantKey(narrowed), narrowed);
  }
  return [...grants.values()];
}
