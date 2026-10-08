import { useMemo } from "react";

import {
  adminCoverage,
  indexServers,
  limitGrantsAnything,
  MCP_CONNECT_SCOPE,
  parseMcpConnectGrant,
} from "./mcpAccessModel";
import type { RoleGrant } from "./types";
import { useOrgMcpServers } from "./useOrgMcpServers";

/**
 * How many servers the role can connect to, for the MCP access tab's label:
 * the servers it names (or every server), plus the ones it administers, less
 * the ones it forbids.
 */
export function useMcpAccessCount(grants: Record<string, RoleGrant>): number {
  const inventory = useOrgMcpServers(true);
  return useMemo(() => {
    const access = parseMcpConnectGrant(grants[MCP_CONNECT_SCOPE]);
    const forbidden = new Set(access.forbidden);
    const coverage = adminCoverage(grants, inventory.groups);
    const reachable = new Set<string>(coverage.keys());
    if (access.allServers) {
      for (const id of indexServers(inventory.groups).keys()) reachable.add(id);
    } else {
      for (const [id, limit] of Object.entries(access.servers)) {
        if (limitGrantsAnything(limit)) reachable.add(id);
      }
    }
    return [...reachable].filter((id) => !forbidden.has(id)).length;
  }, [grants, inventory.groups]);
}
