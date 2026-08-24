import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { useMemo } from "react";
import { buildMemberRows, type MemberRow } from "./memberRows";

/**
 * A gateway's members joined with their backing mcp_servers rows. The server
 * list is project-wide because addMember picks from it too; a member whose
 * server is missing from it still renders, classified unknown.
 */
export function useGatewayMemberRows(metaMcpServerId: string): {
  rows: MemberRow[];
  isLoading: boolean;
  servers: McpServer[];
} {
  const { data: membersResult, isLoading: isLoadingMembers } =
    useMetaMcpMembers({ metaMcpServerId }, undefined, {
      enabled: metaMcpServerId !== "",
    });
  const { data: serversResult, isLoading: isLoadingServers } = useMcpServers(
    {},
    undefined,
    { throwOnError: false },
  );
  const servers = useMemo(
    () => serversResult?.mcpServers ?? [],
    [serversResult],
  );
  const rows = useMemo(
    () => buildMemberRows(membersResult?.members ?? [], servers),
    [membersResult, servers],
  );
  return { rows, isLoading: isLoadingMembers || isLoadingServers, servers };
}
