import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import { invalidateAllGatewaySessions } from "@gram/client/react-query/gatewaySessions.js";
import {
  invalidateAllUserSessionClients,
  useUserSessionClientsInfinite,
} from "@gram/client/react-query/userSessionClients.js";
import {
  invalidateAllUserSessions,
  useUserSessionsInfinite,
} from "@gram/client/react-query/userSessions.js";

import { Link, useLocation, useSearchParams } from "react-router";

import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { ClientDetailSheet } from "./ClientDetailSheet";
import { useDrainedPages, PAGE_FETCH_LIMIT } from "./useDrainedPages";
import { useGatewaySessionListing } from "./useGatewaySessionListing";
import { ViewOrgSessionsButton } from "./ViewOrgSessionsButton";
import { ConnectionsListSection } from "@/components/connections/ConnectionsListSection";

type ClientsAndSessionsTabProps = {
  issuerId: string | undefined;
  attachedSessions?: React.ReactNode;
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
  /**
   * Lists the sessions authorized through a gateway instead of every session
   * `issuerId` issued, filtered and paginated on the server. Without an id the
   * listing covers every gateway in the organization, independent of the
   * working project.
   */
  gateway?: { id?: string };
};

/**
 * Body of the Clients and Sessions tab, shared verbatim by the MCP server and
 * gateway detail pages and the Organization Gateway so they cannot drift.
 *
 * MCP servers list everything their single user_session_issuer issued, pulled
 * in full rather than page by page: the counts and grouping below operate over
 * the whole set, so it has to be in memory to be truthful. Gateways list
 * through the gateway listing instead, whose search and filters run on the
 * server ahead of cursor pagination.
 */
export function ClientsAndSessionsTab({
  gateway,
  ...props
}: ClientsAndSessionsTabProps): JSX.Element {
  return gateway ? (
    <GatewayClientsAndSessions {...props} gatewayId={gateway.id} />
  ) : (
    <IssuerClientsAndSessions {...props} />
  );
}

/**
 * Every registration against the issuer, drained like the issuer's sessions:
 * the client grouping surfaces registrations with no connections, so a page
 * left unloaded would hide one still worth revoking.
 */
function useIssuerClients(issuerId: string | undefined, enabled: boolean) {
  const query = useUserSessionClientsInfinite(
    { userSessionIssuerId: issuerId, limit: PAGE_FETCH_LIMIT },
    undefined,
    { enabled },
  );
  const { isTruncated } = useDrainedPages({
    enabled,
    pageCount: query.data?.pages.length ?? 0,
    hasNextPage: query.hasNextPage,
    isFetchingNextPage: query.isFetchingNextPage,
    isFetchNextPageError: query.isFetchNextPageError,
    fetchNextPage: query.fetchNextPage,
  });
  const clients = useMemo(
    () => query.data?.pages.flatMap((p) => p.result.items) ?? [],
    [query.data],
  );
  return { query, clients, isTruncated };
}

function NoIssuerEmptyState({
  authTabPath,
}: {
  authTabPath: string;
}): JSX.Element {
  const { pathname } = useLocation();
  const authTabHref = pathname.replace(/[^/]+\/?$/, authTabPath);
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

function IssuerClientsAndSessions({
  issuerId,
  originatingMcpServerId,
  authTabPath = "settings",
  attachedSessions,
}: Omit<ClientsAndSessionsTabProps, "gateway">): JSX.Element {
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
  const clients = useIssuerClients(issuerId, hasIssuer);

  const sessionsTruncated = useDrainedPages({
    enabled: hasIssuer,
    pageCount: sessionsQuery.data?.pages.length ?? 0,
    hasNextPage: sessionsQuery.hasNextPage,
    isFetchingNextPage: sessionsQuery.isFetchingNextPage,
    isFetchNextPageError: sessionsQuery.isFetchNextPageError,
    fetchNextPage: sessionsQuery.fetchNextPage,
  }).isTruncated;

  const sessions = useMemo(
    () => sessionsQuery.data?.pages.flatMap((p) => p.result.items) ?? [],
    [sessionsQuery.data],
  );

  // Revoking a registration cascades to every session it issued, and revoking a
  // session changes the tally the registration reports, so both directions have
  // to refresh both listings.
  const onClientOrSessionRevoked = () => {
    void clients.query.refetch();
    void sessionsQuery.refetch();
    void invalidateAllUserSessions(queryClient, { refetchType: "all" });
    void invalidateAllUserSessionClients(queryClient, { refetchType: "all" });
  };

  if (!issuerId) return <NoIssuerEmptyState authTabPath={authTabPath} />;

  return (
    <ClientsAndSessionsBody
      attachedSessions={attachedSessions}
      sessions={sessions}
      clients={clients.clients}
      sessionsTile={{
        title: "Active sessions",
        subtext: "Currently authenticated MCP sessions",
      }}
      agentsSubtext="Registered against this server"
      description="Who is connected to this server, what they connect through, and the upstream providers Speakeasy reaches on their behalf."
      // Counts, search, and sort all run over what was loaded, so a set the
      // cap cut short has to say so rather than read as the whole picture.
      notice={
        sessionsTruncated || clients.isTruncated
          ? `Showing ${sessions.length} sessions and ${clients.clients.length} agents. Later pages were not loaded, so the counts, search, and filters on this tab cover only these.`
          : undefined
      }
      originatingMcpServerId={originatingMcpServerId}
      isPending={sessionsQuery.isPending || clients.query.isPending}
      isError={sessionsQuery.isError && sessions.length === 0}
      onRetry={() => {
        void sessionsQuery.refetch();
        void clients.query.refetch();
      }}
      onRevoked={onClientOrSessionRevoked}
    />
  );
}

function GatewayClientsAndSessions({
  gatewayId,
  issuerId,
  authTabPath = "settings",
  attachedSessions,
}: Omit<ClientsAndSessionsTabProps, "gateway" | "originatingMcpServerId"> & {
  gatewayId: string | undefined;
}): JSX.Element {
  const queryClient = useQueryClient();
  // One gateway's registrations belong to its issuer in its project; the
  // organization-wide listing has neither to scope them to.
  const projectScoped = Boolean(gatewayId);
  // Without an issuer a gateway cannot establish sessions, so nothing is read
  // behind the empty state below.
  const hasNothingToList = projectScoped && !issuerId;
  const listing = useGatewaySessionListing(gatewayId, !hasNothingToList);
  const { query } = listing;
  const clients = useIssuerClients(issuerId, projectScoped && !!issuerId);

  const onClientOrSessionRevoked = () => {
    void invalidateAllGatewaySessions(queryClient, { refetchType: "all" });
    void invalidateAllUserSessions(queryClient, { refetchType: "all" });
    void invalidateAllUserSessionClients(queryClient, { refetchType: "all" });
  };

  if (hasNothingToList) {
    return <NoIssuerEmptyState authTabPath={authTabPath} />;
  }

  const loaded = listing.sessions.length;
  // Filters run on the server, so later pages load on request rather than
  // being drained, and a failed page keeps the rows already shown.
  const loadMore =
    query.hasNextPage || (query.isError && loaded > 0) ? (
      <Stack gap={2} align="center">
        {query.isError && (
          <Text small>
            Couldn’t load sessions. Your loaded results are still shown.
          </Text>
        )}
        <Button
          variant="tertiary"
          size="sm"
          disabled={query.isFetching}
          onClick={() =>
            void (query.isError && !query.isFetchNextPageError
              ? query.refetch()
              : query.fetchNextPage())
          }
        >
          {query.isFetching
            ? "Loading…"
            : query.isError
              ? "Retry"
              : "Load more"}
        </Button>
      </Stack>
    ) : undefined;

  return (
    <ClientsAndSessionsBody
      attachedSessions={attachedSessions}
      sessions={listing.sessions}
      clients={projectScoped ? clients.clients : undefined}
      sessionsTile={{
        title: listing.sessionsTileTitle,
        subtext: listing.isFiltered
          ? "Matching the current search and filters"
          : "Currently authenticated MCP sessions",
      }}
      agentsSubtext="Registered against this gateway"
      description="Who is connected through this gateway, what they connect through, and the upstream providers Speakeasy reaches on their behalf."
      notice={
        query.hasNextPage
          ? `Showing the first ${loaded} sessions. The counts cover only the sessions loaded so far.`
          : undefined
      }
      toolbar={listing.toolbar}
      footer={loadMore}
      isPending={query.isPending || (projectScoped && clients.query.isPending)}
      isError={query.isError && loaded === 0}
      onRetry={() => {
        void query.refetch();
        if (projectScoped) void clients.query.refetch();
      }}
      onRevoked={onClientOrSessionRevoked}
      emptyHeading={
        listing.isFiltered ? "No connections match these filters" : undefined
      }
      emptyDescription={
        listing.isFiltered
          ? "Clear the search or filters to see every connection."
          : "Connections agents establish through this gateway will appear here."
      }
      projectScoped={projectScoped}
    />
  );
}

function ClientsAndSessionsBody({
  attachedSessions,
  sessions,
  clients,
  sessionsTile,
  agentsSubtext,
  description,
  notice,
  toolbar,
  footer,
  originatingMcpServerId,
  isPending,
  isError,
  onRetry,
  onRevoked,
  emptyHeading,
  emptyDescription,
  projectScoped = true,
}: {
  attachedSessions?: React.ReactNode;
  sessions: UserSession[];
  /** Absent where no registrations can be listed, which drops their tile. */
  clients?: UserSessionClient[];
  sessionsTile: { title: string; subtext: string };
  agentsSubtext: string;
  description: string;
  notice?: string;
  toolbar?: React.ReactNode;
  footer?: React.ReactNode;
  originatingMcpServerId?: string;
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  onRevoked: () => void;
  emptyHeading?: string;
  emptyDescription?: string;
  /**
   * Whether these connections belong to the working project. A listing that
   * spans projects offers no revocation, registration details, or link to the
   * project's sessions, since each would act through the wrong project.
   */
  projectScoped?: boolean;
}): JSX.Element {
  // Command K links straight to a registration that may sit on a page of
  // connections not yet loaded, so the sheet opens from the URL rather than
  // from a row.
  const [params, setParams] = useSearchParams();
  const registrationId = projectScoped ? params.get("registration") : null;
  const closeRegistration = () =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.delete("registration");
        return next;
      },
      { replace: true },
    );

  return (
    <Stack gap={6}>
      {attachedSessions}
      <StatTileGroup>
        <StatTile
          title={sessionsTile.title}
          value={sessions.length}
          tone="information"
          icon="activity"
          subtext={sessionsTile.subtext}
        />
        {clients && (
          <StatTile
            title="Agents"
            value={clients.length}
            tone="information"
            icon="app-window"
            subtext={agentsSubtext}
          />
        )}
      </StatTileGroup>

      {notice && (
        <Text small muted>
          {notice}
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
              {description}
            </Text>
          </Stack>
          {projectScoped && <ViewOrgSessionsButton />}
        </Stack>

        {toolbar}

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
          isPending={isPending}
          isError={isError}
          onRetry={onRetry}
          onRevoked={onRevoked}
          // Every session here was issued by this server or gateway, so filing
          // them by MCP server draws one group and an option that appears broken.
          hideGroupings={["issuer"]}
          emptyHeading={emptyHeading}
          emptyDescription={emptyDescription}
          revocable={projectScoped}
          canViewRegistrations={projectScoped}
        />

        {footer}
      </Stack>

      {registrationId && (
        <ClientDetailSheet
          clientId={registrationId}
          client={clients?.find((client) => client.id === registrationId)}
          open
          onOpenChange={(open) => {
            if (!open) closeRegistration();
          }}
        />
      )}
    </Stack>
  );
}
