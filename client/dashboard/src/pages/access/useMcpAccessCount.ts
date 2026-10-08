import { useMemo } from "react";

import {
  adminCoverage,
  limitGrantsAnything,
  MCP_CONNECT_SCOPE,
  parseMcpConnectGrant,
} from "./mcpAccessModel";
import type { Selector } from "@gram/client/models/components/selector.js";
import type { RoleGrant } from "./types";
import { useOrgMcpServers } from "./useOrgMcpServers";

/** Whether a stored selector reaches this server, whole or in part. */
function selectorNamesServer(
  s: Selector,
  serverId: string,
  projectId: string,
): boolean {
  if (s.resourceKind !== "mcp") return false;
  if (s.resourceId !== serverId && s.resourceId !== "*") return false;
  return !s.projectId || s.projectId === "*" || s.projectId === projectId;
}

/**
 * How many servers the role can connect to, for the MCP access tab's label:
 * the servers it names (or every server), the projects it names, and the
 * servers it administers, less the ones it blocks outright. Null until the
 * organization's servers have loaded, so the label never shows a wrong count.
 */
export function useMcpAccessCount(
  grants: Record<string, RoleGrant>,
  /** False while the editor is closed, so a hidden editor loads nothing. */
  enabled: boolean,
): number | null {
  const inventory = useOrgMcpServers(enabled);
  return useMemo(() => {
    if (!inventory.settled) return null;
    const access = parseMcpConnectGrant(grants[MCP_CONNECT_SCOPE]);
    if (access.denyAll) return 0;
    const coverage = adminCoverage(grants, inventory.groups);
    const forbidden = new Set(access.forbidden);
    let count = 0;
    for (const group of inventory.groups) {
      for (const server of group.servers) {
        const named = access.servers[server.id];
        const reached =
          coverage.has(server.id) ||
          !!access.allServers ||
          (!!named && limitGrantsAnything(named)) ||
          access.preservedAllow.some((s) =>
            selectorNamesServer(s, server.id, group.projectId),
          );
        // Only a block on the whole server takes it away; one naming a tool
        // or an annotation leaves the rest.
        const blocked =
          forbidden.has(server.id) ||
          access.preservedDeny.some(
            (s) =>
              !s.tool &&
              !s.disposition &&
              selectorNamesServer(s, server.id, group.projectId),
          );
        if (reached && !blocked) count++;
      }
    }
    return count;
  }, [grants, inventory.groups, inventory.settled]);
}
