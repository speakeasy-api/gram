import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";

/**
 * What the server attachment block decides without rendering: which servers
 * a platform token can be minted for, and whether an agent's policy reaches
 * one.
 */

/**
 * Servers the issuer can be attached to: a disabled server serves nothing, a
 * private-only one does not advertise the grant, and an issuer a project owns
 * may only be attached within that project, which the server enforces too.
 * An organization issuer (empty project id) spans every project.
 */
export function attachableServers(
  servers: McpServer[],
  issuerProjectId: string,
): McpServer[] {
  return servers.filter(
    (server) =>
      server.visibility !== "disabled" &&
      server.networkAccessMode !== "private_only" &&
      (issuerProjectId === "" || server.projectId === issuerProjectId),
  );
}

/**
 * The resource the server's mcp:write and mcp:connect checks name: the
 * toolset of a toolset-backed server, else the server itself.
 */
export function permissionResourceId(server: McpServer): string {
  return server.toolsetId ?? server.id;
}

/**
 * Whether an agent's configured policy holds an allow grant for connecting to
 * the server: one naming the server's own id (the id the runtime checks
 * mcp:connect against) or every server, with a project filter naming the
 * server's project, every project, or none.
 *
 * This reads configured grants only. Effective access is decided by the
 * server on each connection, from the whole policy, the owner's delegable
 * ceiling and any exclusions, so a grant here is guidance on what to look at,
 * never proof that a connection will be admitted.
 */
export function policyGrantsConnect(
  grants: AgentPolicyGrant[],
  server: McpServer,
): boolean {
  return grants.some(
    (grant) =>
      grant.scope === "mcp:connect" &&
      grant.effect === "allow" &&
      grant.selector.resourceKind === "mcp" &&
      (grant.selector.resourceId === "*" ||
        grant.selector.resourceId === server.id) &&
      (grant.selector.projectId === undefined ||
        grant.selector.projectId === "" ||
        grant.selector.projectId === "*" ||
        grant.selector.projectId === server.projectId),
  );
}
