import { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import type { QueryParamStatus as ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import {
  invalidateAllUserSessionClients,
  useUserSessionClientsInfinite,
} from "@gram/client/react-query/userSessionClients.js";
import {
  invalidateAllUserSessions,
  useUserSessionsInfinite,
} from "@gram/client/react-query/userSessions.js";

import { MetricCard, MetricCardGroup } from "@/components/chart/MetricCard";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { clientDocumentOrigin } from "@/lib/user-session-client-source";
import { UserSessionClientsList } from "./UserSessionClientsList";
import { UserSessionsList } from "./UserSessionsList";
import { useDrainedPages, PAGE_FETCH_LIMIT } from "./useDrainedPages";
import { ViewOrgSessionsButton } from "./ViewOrgSessionsButton";

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
  // The filter remembers which issuer it was set against. Navigating between
  // two MCP servers reuses this component instance rather than remounting it
  // (and when the target server is already in the query cache there is no
  // loading gap to unmount through), so a filter keyed only by client id would
  // survive onto the next server and silently scope its sessions to a client
  // that belongs to a different issuer. Derived during render rather than
  // reset in an effect, so there is no stale first paint.
  const [filter, setFilter] = useState<{
    issuerId: string;
    client: UserSessionClient;
  } | null>(null);
  // Compare on `filter &&` rather than `filter?.issuerId === issuerId`: with
  // no filter set and no issuer attached, the optional-chained form compares
  // undefined to undefined, takes the truthy branch, and dereferences null.
  const activeFilter =
    filter && filter.issuerId === issuerId ? filter.client : null;
  // client_name is client-chosen; pair it with the unspoofable document origin
  // so the chip cannot be made to read like a client it isn't.
  const filterOrigin = activeFilter ? clientDocumentOrigin(activeFilter) : null;

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

  // Offered by the sessions table's Client filter. Keyed by id rather than
  // name because client_name is client-chosen and two registrations can pick
  // the same one.
  const clientOptions = useMemo(
    () =>
      clients
        .map((client) => ({ value: client.id, label: client.clientName }))
        .sort((a, b) => a.label.localeCompare(b.label)),
    [clients],
  );

  const selectClient = (client: UserSessionClient) => {
    if (!issuerId) return;
    setFilter({ issuerId, client });
  };

  // Revoking a client cascades to every session it issued, so the sessions
  // list has to be refetched too or it keeps offering Revoke actions for
  // sessions the backend already deleted. Only the filter pointing at the
  // revoked client is cleared; revoking some other client leaves the current
  // drill-down where the operator put it.
  const onClientRevoked = (revoked: UserSessionClient) => {
    setFilter((current) =>
      current?.client.id === revoked.id ? null : current,
    );
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
      <MetricCardGroup>
        <MetricCard
          title="Active sessions"
          value={sessions.length}
          tone="information"
          icon="activity"
          accentColor="blue"
          subtext="Currently authenticated MCP sessions"
        />
        <MetricCard
          title="Clients"
          value={clients.length}
          tone="information"
          icon="app-window"
          accentColor="purple"
          subtext="Registered against this server"
        />
      </MetricCardGroup>

      {/* Counts, search, and sort all run over what was loaded, so a set the
          cap cut short has to say so rather than read as the whole picture. */}
      {isTruncated && (
        <Text small muted>
          Showing {sessions.length} sessions and {clients.length} clients. Later
          pages were not loaded, so the counts, search, and filters on this tab
          cover only these.
        </Text>
      )}

      {/* Both listings below are narrowed to the selected client, which is what
          keeps them short enough to read together without scrolling. */}
      {activeFilter && (
        <div className="border-border bg-muted/30 flex items-center justify-between gap-3 border px-3 py-2">
          <Text small>
            Filtered to {activeFilter.clientName}
            {filterOrigin ? ` (${filterOrigin})` : ""}
          </Text>
          <Button variant="tertiary" size="sm" onClick={() => setFilter(null)}>
            Clear filter
          </Button>
        </div>
      )}

      <Stack gap={3}>
        <Stack
          direction="horizontal"
          align="start"
          justify="space-between"
          gap={4}
        >
          <Stack gap={1}>
            <Text variant="subheading">Sessions</Text>
            <Text small muted>
              Active sessions for this MCP Server.
            </Text>
          </Stack>
          <ViewOrgSessionsButton />
        </Stack>
        <UserSessionsList
          sessions={sessions}
          isPending={sessionsQuery.isPending}
          isError={sessionsQuery.isError && sessions.length === 0}
          onRetry={() => void sessionsQuery.refetch()}
          onRevoked={onSessionRevoked}
          clientFilterId={activeFilter?.id}
          clientOptions={clientOptions}
        />
      </Stack>

      <Stack gap={3}>
        <Stack gap={1}>
          <Text variant="subheading">Clients</Text>
          <Text small muted>
            MCP Clients, agents, and other applications that are registered for
            creating sessions.
          </Text>
        </Stack>
        <UserSessionClientsList
          clients={clients}
          isPending={clientsQuery.isPending}
          isError={clientsQuery.isError && clients.length === 0}
          onRetry={() => void clientsQuery.refetch()}
          filteredClientId={activeFilter?.id}
          onViewSessions={selectClient}
          onClientRevoked={onClientRevoked}
        />
      </Stack>
    </Stack>
  );
}
