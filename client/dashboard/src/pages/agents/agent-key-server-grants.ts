import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import {
  ANY_RESOURCE,
  buildRequestedGrants,
  delegableGrantKey,
} from "./agent-api-key-grants";

export interface CredentialServerResource {
  resourceId: string;
  projectId: string;
  kind: string;
}

/**
 * Expand only discovered MCP grants into exact, selected server/project pairs.
 * Use the existing narrowing validator, never manufacture scopes or replace
 * pinned dimensions. The remaining tool/disposition dimensions stay available
 * to AgentGrantSelector. Unproxied servers do not consume Gram credentials.
 */
export function narrowGrantsToServers(
  grants: AgentPolicyGrantForm[],
  servers: CredentialServerResource[],
): AgentPolicyGrantForm[] {
  const narrowed = new Map<string, AgentPolicyGrantForm>();
  for (const grant of grants) {
    if (
      grant.effect !== "allow" ||
      grant.scope !== "mcp:connect" ||
      grant.selector.resourceKind !== "mcp"
    )
      continue;
    for (const server of servers) {
      if (server.kind === "Unproxied") continue;
      if (
        !server.resourceId ||
        !server.projectId ||
        (grant.selector.resourceId !== ANY_RESOURCE &&
          grant.selector.resourceId !== server.resourceId) ||
        (grant.selector.projectId !== undefined &&
          grant.selector.projectId !== ANY_RESOURCE &&
          grant.selector.projectId !== server.projectId)
      )
        continue;
      const [candidate] = buildRequestedGrants([
        {
          grant,
          narrowing: {
            resourceId: server.resourceId,
            projectId: server.projectId,
          },
        },
      ]);
      if (candidate) {
        const scoped = {
          ...candidate,
          selector: { ...candidate.selector, projectId: server.projectId },
        };
        narrowed.set(delegableGrantKey(scoped), scoped);
      }
    }
  }
  return [...narrowed.values()];
}
