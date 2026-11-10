import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
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
  isError: boolean;
  membersUpdatedAt: number;
  refetch: () => Promise<unknown>;
} {
  const {
    data: membersResult,
    isLoading: isLoadingMembers,
    isError: membersFailed,
    dataUpdatedAt: membersUpdatedAt,
    isFetching: fetchingMembers,
    refetch: refetchMembers,
  } = useMetaMcpMembers({ metaMcpServerId }, undefined, {
    enabled: metaMcpServerId !== "",
    throwOnError: false,
  });
  const gramProject = useProjectSlugForRequests();
  const {
    data: serversResult,
    isLoading: isLoadingServers,
    isError: serversFailed,
    refetch: refetchServers,
  } = useMcpServers({ gramProject }, undefined, { throwOnError: false });
  const servers = useMemo(
    () => serversResult?.mcpServers ?? [],
    [serversResult],
  );
  const rows = useMemo(
    () => buildMemberRows(membersResult?.members ?? [], servers),
    [membersResult, servers],
  );
  return {
    rows,
    // Cached data on a failed/in-flight refresh is not authoritative.
    membersUpdatedAt: membersFailed || fetchingMembers ? 0 : membersUpdatedAt,
    isLoading: isLoadingMembers || isLoadingServers,
    servers,
    isError: membersFailed || serversFailed,
    refetch: () => Promise.all([refetchMembers(), refetchServers()]),
  };
}
