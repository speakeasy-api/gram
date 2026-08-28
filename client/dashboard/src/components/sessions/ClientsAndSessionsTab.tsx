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

import { Link, useLocation } from "react-router";

import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
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
  originatingMcpServerId,
  authTabPath = "settings",
}: {
  issuerId: string | undefined;
  /** Canonical mcp_servers.id only; never an issuer, toolset, or gateway id. */
  originatingMcpServerId?: string;
  /**
   * Sibling tab where authentication is configured, for the no-issuer empty
   * state to point at. Differs between the two server pages this tab is shared
   * by — the toolset-backed one has a dedicated Authentication tab, while the
   * mcp_servers-backed one keeps auth inside Settings — so the destination is
   * the caller's to name rather than something this component can infer.
   */
  authTabPath?: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { pathname } = useLocation();
  const authTabHref = pathname.replace(/[^/]+\/?$/, authTabPath);
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
  // Revoking a registration cascades to every session it issued, and revoking a
  // session changes the tally the registration reports, so both directions have
  // to refresh both listings.
  const onClientOrSessionRevoked = () => {
    void clientsQuery.refetch();
    void sessionsQuery.refetch();
    void invalidateAllUserSessions(queryClient, { refetchType: "all" });
    void invalidateAllUserSessionClients(queryClient, { refetchType: "all" });
  };

  if (!issuerId) {
    return (
      <InlineEmptyState
        icon="unplug"
        heading="No connections can be made yet"
        description="This server has no session issuer, so no agent can register against it and no session can be established. Turn on authentication to start accepting OAuth connections."
        action={
          // The last path segment is swapped rather than using a `../` link:
          // these tabs are a switch on a route param, not nested routes, so
          // both route- and path-relative `..` climb past the server slug and
          // land on the MCP index.
          <Link to={authTabHref} className="hover:no-underline">
            <Button variant="secondary" size="sm">
              <Button.Text>Configure authentication</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" size="small" />
              </Button.RightIcon>
            </Button>
          </Link>
        }
      />
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
          title="Agents"
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
          Showing {sessions.length} sessions and {clients.length} agents. Later
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

        {/* Registrations are handed in rather than listed separately: the
            client grouping is the same inventory, and a registration with no
            connections still appears there so it stays visible and revocable. */}
        <ConnectionsListSection
          sessions={sessions}
          clients={clients}
          killswitchContext={{
            capabilityKey: "mcp_tool_calls",
            originatingMcpServerId,
          }}
          isPending={sessionsQuery.isPending || clientsQuery.isPending}
          isError={sessionsQuery.isError && sessions.length === 0}
          onRetry={() => {
            void sessionsQuery.refetch();
            void clientsQuery.refetch();
          }}
          onRevoked={onClientOrSessionRevoked}
        />
      </Stack>
    </Stack>
  );
}
