import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { QueryParamStatus as ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import {
  invalidateAllUserSessionClients,
  useUserSessionClientsInfinite,
} from "@gram/client/react-query/userSessionClients.js";
import {
  invalidateAllUserSessions,
  useUserSessionsInfinite,
} from "@gram/client/react-query/userSessions.js";

import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { UserSessionClientsList } from "./UserSessionClientsList";
import { useDrainedPages, PAGE_FETCH_LIMIT } from "./useDrainedPages";
import { ViewOrgSessionsButton } from "./ViewOrgSessionsButton";
import { ConnectionsListSection } from "@/components/connections/ConnectionsListSection";

/**
 * Body of the Clients and Sessions tab, shared verbatim by the toolset-backed
 * and mcp_servers-backed MCP server detail pages so the two cannot drift.
 *
 * Both listings are scoped to the server's user_session_issuer, of which a
 * server has exactly one. Both are also pulled in full rather than page by
 * page: the counts, search, filters, sort, and pagination below all operate
 * over the whole set, so it has to be in memory to be truthful.
 */
export function ClientsAndSessionsTab({
  issuerId,
}: {
  issuerId: string | undefined;
}): JSX.Element {
  const queryClient = useQueryClient();
  // Both queries treat a missing user_session_issuer_id as "don't filter by
  // issuer", so leaving them enabled on a server with no issuer would list the
  // whole project's sessions and clients under one server's tab. The hooks
  // can't move behind the early return below, so they are disabled instead.
  const hasIssuer = Boolean(issuerId);
  const sessionsQuery = useUserSessionsInfinite(
    {
      userSessionIssuerId: issuerId,
      status: "active" as ListUserSessionsQueryParamStatus,
      limit: PAGE_FETCH_LIMIT,
    },
    undefined,
    { enabled: hasIssuer },
  );
  const clientsQuery = useUserSessionClientsInfinite(
    { userSessionIssuerId: issuerId, limit: PAGE_FETCH_LIMIT },
    undefined,
    { enabled: hasIssuer },
  );

  const sessionsTruncated = useDrainedPages({
    enabled: hasIssuer,
    pageCount: sessionsQuery.data?.pages.length ?? 0,
    hasNextPage: sessionsQuery.hasNextPage,
    isFetchingNextPage: sessionsQuery.isFetchingNextPage,
    isFetchNextPageError: sessionsQuery.isFetchNextPageError,
    fetchNextPage: sessionsQuery.fetchNextPage,
  }).isTruncated;
  const clientsTruncated = useDrainedPages({
    enabled: hasIssuer,
    pageCount: clientsQuery.data?.pages.length ?? 0,
    hasNextPage: clientsQuery.hasNextPage,
    isFetchingNextPage: clientsQuery.isFetchingNextPage,
    isFetchNextPageError: clientsQuery.isFetchNextPageError,
    fetchNextPage: clientsQuery.fetchNextPage,
  }).isTruncated;

  const sessions = useMemo(
    () => sessionsQuery.data?.pages.flatMap((p) => p.result.items) ?? [],
    [sessionsQuery.data],
  );
  const clients = useMemo(
    () => clientsQuery.data?.pages.flatMap((p) => p.result.items) ?? [],
    [clientsQuery.data],
  );

  // Revoking a client cascades to every session it issued, so the sessions
  // list has to be refetched too or it keeps offering Revoke actions for
  // sessions the backend already deleted. Only the filter pointing at the
  // revoked client is cleared; revoking some other client leaves the current
  // drill-down where the operator put it.
  const onClientRevoked = () => {
    void clientsQuery.refetch();
    void invalidateAllUserSessions(queryClient, { refetchType: "all" });
  };

  // The clients table reports a live-session tally per client, which a session
  // revoke just decremented. It has no way to learn that on its own, so it
  // would keep advertising a session that no longer exists.
  const onSessionRevoked = () => {
    void sessionsQuery.refetch();
    void invalidateAllUserSessionClients(queryClient, { refetchType: "all" });
  };

  if (!issuerId) {
    return (
      <Text muted small>
        This server has no session issuer, so no clients can register against it
        and no sessions can be established. Configure authentication to start
        accepting OAuth connections.
      </Text>
    );
  }

  const isTruncated = sessionsTruncated || clientsTruncated;

  return (
    <Stack gap={6}>
      <StatTileGroup>
        <StatTile
          title="Active sessions"
          value={sessions.length}
          tone="information"
          icon="activity"
          subtext="Currently authenticated MCP sessions"
        />
        <StatTile
          title="Clients"
          value={clients.length}
          tone="information"
          icon="app-window"
          subtext="Registered against this server"
        />
      </StatTileGroup>

      {/* Counts, search, and sort all run over what was loaded, so a set the
          cap cut short has to say so rather than read as the whole picture. */}
      {isTruncated && (
        <Text small muted>
          Showing {sessions.length} sessions and {clients.length} clients. Later
          pages were not loaded, so the counts, search, and filters on this tab
          cover only these.
        </Text>
      )}

      <Stack gap={3}>
        <Stack
          direction="horizontal"
          align="start"
          justify="space-between"
          gap={4}
        >
          <Stack gap={1}>
            <Text variant="subheading">Connections</Text>
            <Text small muted>
              Who is connected to this server, what they connect through, and
              the upstream providers Gram reaches on their behalf.
            </Text>
          </Stack>
          <ViewOrgSessionsButton />
        </Stack>

        <ConnectionsListSection
          sessions={sessions}
          isPending={sessionsQuery.isPending}
          isError={sessionsQuery.isError && sessions.length === 0}
          onRetry={() => void sessionsQuery.refetch()}
          onRevoked={onSessionRevoked}
        />
      </Stack>

      <Stack gap={3}>
        <Stack gap={1}>
          <Text variant="subheading">Registered clients</Text>
          <Text small muted>
            MCP Clients, agents, and other applications that are registered for
            creating sessions. Revoking a registration cuts off every connection
            it established.
          </Text>
        </Stack>
        <UserSessionClientsList
          clients={clients}
          isPending={clientsQuery.isPending}
          isError={clientsQuery.isError && clients.length === 0}
          onRetry={() => void clientsQuery.refetch()}
          onClientRevoked={onClientRevoked}
        />
      </Stack>
    </Stack>
  );
}
