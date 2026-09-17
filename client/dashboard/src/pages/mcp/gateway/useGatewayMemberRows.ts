import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useEffect, useMemo, useRef } from "react";
import {
  buildMemberRows,
  type MemberRow,
  type AddBatchState,
} from "./memberRows";

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
  serversUpdatedAt: number;
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
    isFetching: fetchingServers,
    dataUpdatedAt: serversUpdatedAt,
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
    serversUpdatedAt:
      serversFailed || fetchingServers || isLoadingServers
        ? 0
        : serversUpdatedAt,
    isLoading: isLoadingMembers || isLoadingServers,
    servers,
    isError: membersFailed || serversFailed,
    refetch: () => Promise.all([refetchMembers(), refetchServers()]),
  };
}

/** Reconcile only authoritative full lists received after the batch's writes. */
export function useReconcileWrappers(
  state: AddBatchState,
  servers: McpServer[],
  serversUpdatedAt: number,
  mutating: boolean,
): () => void {
  const reconciledAt = useRef(serversUpdatedAt);
  const latestUpdatedAt = useRef(serversUpdatedAt);
  useEffect(() => {
    if (serversUpdatedAt) latestUpdatedAt.current = serversUpdatedAt;
    if (
      mutating ||
      !serversUpdatedAt ||
      serversUpdatedAt === reconciledAt.current
    )
      return;
    reconciledAt.current = serversUpdatedAt;
    // useMcpServers is an unfiltered, unpaginated project-wide list.
    const liveIds = new Set(servers.map((server) => server.id));
    for (const [key, id] of state.wrappers) {
      if (liveIds.has(id)) continue;
      state.wrappers.delete(key);
      // Failed attachments retain their allocated retry slot.
      if (state.completed.delete(key)) state.orders.delete(key);
    }
  }, [state, servers, serversUpdatedAt, mutating]);
  // Called after writes, before invalidation: responses received mid-batch
  // cannot prove that a newly created wrapper has been deleted.
  return () => {
    reconciledAt.current = latestUpdatedAt.current;
  };
}
