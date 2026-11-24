import { MCPSessionsEmptyState } from "@/components/setup-empty-state";
import { useSessionAgents } from "@/hooks/useSessionAgents";
import { useDeferredValue, useMemo, useState } from "react";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { ConnectionsList } from "@/components/connections/ConnectionsList";
import {
  CONNECTION_GROUPING_LABELS,
  type ConnectionGrouping,
} from "@/components/connections/groupConnections";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { subjectLabel } from "@/lib/user-session-status";
import { useUserSessionFacets } from "@gram/client/react-query/userSessionFacets.js";
import { useUserSessionsInfinite } from "@gram/client/react-query/userSessions.js";
import type { ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import { useUserSessionClientsInfinite } from "@gram/client/react-query/userSessionClients.js";
import { RegisterClientAction } from "@/components/sessions/RegisterClientAction";
import {
  PAGE_FETCH_LIMIT,
  useDrainedPages,
} from "@/components/sessions/useDrainedPages";

const USER_SESSION_FILTERS = defineFilters([
  // Unpinned: the list already splits itself into active and inactive, so a
  // status chip sat in the bar restating the layout. Still reachable under
  // "More filters" for the narrower cuts the split does not make — expired
  // versus revoked — and it re-pins itself as a chip once set.
  { id: "status", label: "Status", kind: "select" },
  // The provider is the header's picker, not a filter here.
  // "User", not "Identity": the facets endpoint lists only `user:` subjects,
  // so the wider label offered agents and keys the filter cannot select.
  { id: "subjectUrn", label: "User", kind: "select", pinned: true },
]);

/** With connection health the list names people, as Connection Logs does. */
const HEALTH_SESSION_FILTERS = defineFilters([
  { id: "status", label: "Status", kind: "select" },
  {
    id: "subjectUrn",
    label: "Person",
    kind: "select",
    pinned: true,
    allLabel: "All people",
  },
]);

const STATUS_TOOLBAR_OPTIONS = [
  { value: "active", label: "Active" },
  { value: "expired", label: "Expired" },
  { value: "revoked", label: "Revoked" },
];

const GROUPING_OPTIONS: { value: ConnectionGrouping; label: string }[] = [
  { value: "subject", label: CONNECTION_GROUPING_LABELS.subject },
  { value: "provider", label: CONNECTION_GROUPING_LABELS.provider },
  { value: "client", label: CONNECTION_GROUPING_LABELS.client },
];

const CONNECTIONS_DESCRIPTION =
  "Connections Speakeasy brokers through this provider. Revoking ends current sessions immediately, but clients can authenticate and reconnect. A killswitch is a separate action that blocks matching MCP tool calls without ending sessions; revocation never creates or lifts one.";

/**
 * One provider's connections: today's MCP Sign-in list, narrowed to the
 * provider the header's picker chose.
 */
export function ConnectionsTab({
  providerId,
  health,
}: {
  providerId: string;
  /** Connection health's Active Sessions: its count, toolbar and groups. */
  health?: { activeNow: number | undefined; refetch: () => void };
}): JSX.Element {
  const project = useProject();
  const { hasScope } = useRBAC();
  const filters = useFilterState(USER_SESSION_FILTERS);
  const projectSlug = project.slug;
  const canRevoke = hasScope("project:write", project.id);
  const [searchQuery, setSearchQuery] = useState("");
  const [grouping, setGrouping] = useState<ConnectionGrouping>("subject");
  const activeNow = health?.activeNow;

  const handleFilterChange = (id: string, value: FilterValue) => {
    filters.setValue(id as keyof typeof filters.values, value as never);
  };

  const { data: facets } = useUserSessionFacets({ gramProject: projectSlug });

  const optionsById: OptionsById = useMemo(
    () => ({
      status: STATUS_TOOLBAR_OPTIONS,
      subjectUrn: (facets?.users ?? []).map((u) => ({
        value: u.value,
        label: u.displayName,
      })),
    }),
    [facets],
  );

  const {
    data,
    isPending,
    isError,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    isFetching,
    refetch,
  } = useUserSessionsInfinite({
    gramProject: projectSlug,
    status: (filters.values.status ?? undefined) as
      | ListUserSessionsQueryParamStatus
      | undefined,
    subjectUrn: filters.values.subjectUrn ?? undefined,
    userSessionIssuerId: providerId,
  });
  const unresolvedSessions = useMemo(
    () => data?.pages.flatMap((p) => p.result.items) ?? [],
    [data],
  );

  const sessions = useSessionAgents(unresolvedSessions);

  // The provider's registrations, drained like the MCP server tab drains
  // them: the agent grouping lists a registration with no sessions yet, so an
  // agent registered here shows up, and its sheet opens, before it signs in.
  const clientsQuery = useUserSessionClientsInfinite({
    gramProject: projectSlug,
    userSessionIssuerId: providerId,
    limit: PAGE_FETCH_LIMIT,
  });
  const { isTruncated: clientsTruncated } = useDrainedPages({
    enabled: true,
    pageCount: clientsQuery.data?.pages.length ?? 0,
    hasNextPage: clientsQuery.hasNextPage,
    isFetchingNextPage: clientsQuery.isFetchingNextPage,
    isFetchNextPageError: clientsQuery.isFetchNextPageError,
    fetchNextPage: clientsQuery.fetchNextPage,
  });
  const clients = useMemo(
    () => clientsQuery.data?.pages.flatMap((p) => p.result.items) ?? [],
    [clientsQuery.data],
  );
  const reload = () => {
    void refetch();
    void clientsQuery.refetch();
    health?.refetch();
  };

  // Search filters the loaded rows client-side (subject / client / server /
  // upstream provider), matching the loaded-count semantics shown in the
  // toolbar. Deferred so the input stays responsive while the list re-filters.
  const deferredSearch = useDeferredValue(searchQuery);
  const filteredSessions = useMemo(() => {
    const q = deferredSearch.trim().toLowerCase();
    if (!q) return sessions;
    return sessions.filter(
      (s) =>
        subjectLabel(s).toLowerCase().includes(q) ||
        (s.clientName ?? "").toLowerCase().includes(q) ||
        s.issuerSlug.toLowerCase().includes(q) ||
        (s.upstreams ?? []).some((upstream) =>
          upstream.issuerSlug.toLowerCase().includes(q),
        ),
    );
  }, [sessions, deferredSearch]);
  // Search narrows registrations by name too, so an agent with no sessions
  // yet can still be found under the agent grouping.
  const filteredClients = useMemo(() => {
    const q = deferredSearch.trim().toLowerCase();
    if (!q) return clients;
    return clients.filter((c) => c.clientName.toLowerCase().includes(q));
  }, [clients, deferredSearch]);
  // Only the agent grouping seeds a row per registration; the others are cut
  // from sessions alone, so registrations add nothing to them.
  const listsRegistrations = grouping === "client";
  const hasRows =
    filteredSessions.length > 0 ||
    (listsRegistrations && filteredClients.length > 0);

  let listBody: JSX.Element;
  if (isPending || clientsQuery.isPending) {
    listBody = (
      <div className="space-y-2">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="h-20 w-full" />
        ))}
      </div>
    );
  } else if (
    (isError && sessions.length === 0) ||
    (clientsQuery.isError && clients.length === 0)
  ) {
    listBody = (
      <div className="flex items-center justify-between gap-3">
        <p className="text-destructive text-sm">
          Couldn&apos;t load connections.
        </p>
        <Button variant="tertiary" size="sm" onClick={reload}>
          Retry
        </Button>
      </div>
    );
  } else if (
    sessions.length === 0 &&
    Object.values(filters.values).some(Boolean)
  ) {
    listBody = (
      <p className="text-muted-foreground text-sm">
        No connections match these filters
      </p>
    );
  } else if (sessions.length === 0 && clients.length === 0) {
    listBody = <MCPSessionsEmptyState />;
  } else if (!hasRows) {
    listBody = (
      <p className="text-muted-foreground text-sm">
        {deferredSearch.trim()
          ? "No connections match your search"
          : "No one has signed in through this provider yet. Group by Agent to see its registered agents."}
      </p>
    );
  } else {
    listBody = (
      <ConnectionsList
        sessions={filteredSessions}
        grouping={grouping}
        canRevoke={canRevoke}
        onRevoked={reload}
        clients={filteredClients}
        killswitchContext={{ capabilityKey: "mcp_tool_calls" }}
        project={project}
        splitNeedsReauth={!!health}
      />
    );
  }

  return (
    <div className="space-y-4">
      {health ? (
        // No description here, so the row collapses when the action is hidden.
        <div className="flex justify-end empty:hidden">
          <RegisterClientAction issuerId={providerId} />
        </div>
      ) : (
        <div className="flex items-start justify-between gap-4">
          <p className="text-muted-foreground max-w-3xl text-sm">
            {CONNECTIONS_DESCRIPTION}
          </p>
          <RegisterClientAction issuerId={providerId} />
        </div>
      )}
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={searchQuery}
          onChange={setSearchQuery}
          debounceMs={150}
          placeholder="Search connections"
          className={health ? "w-52" : undefined}
        />
        <Page.Toolbar.Filters
          schema={health ? HEALTH_SESSION_FILTERS : USER_SESSION_FILTERS}
          values={filters.values}
          optionsById={optionsById}
          onChange={handleFilterChange}
          onClear={filters.clearValue as (id: string) => void}
          onClearAll={filters.clearAll}
        />
        {health ? (
          <Page.Toolbar.Leading>
            <span className="text-eyebrow ml-2">Group by</span>
            <SegmentedControl
              ariaLabel="Group by"
              value={grouping}
              onChange={(value: string) =>
                setGrouping(value as ConnectionGrouping)
              }
              options={GROUPING_OPTIONS}
            />
          </Page.Toolbar.Leading>
        ) : null}
        {health && activeNow !== undefined ? (
          <Page.Toolbar.Actions>
            <span className="text-muted-foreground text-sm whitespace-nowrap">
              {activeNow.toLocaleString()} active{" "}
              {activeNow === 1 ? "session" : "sessions"}
            </span>
          </Page.Toolbar.Actions>
        ) : null}
        <Page.Toolbar.Refresh
          onRefresh={reload}
          isRefreshing={isFetching || clientsQuery.isFetching}
        />
      </Page.Toolbar>

      {/* Outside the bar: the toolbar narrows which connections are listed,
        while grouping re-cuts the ones that survived into a different
        shape. Sitting it directly above the table it restructures — and
        matching where the MCP server tab puts the same control — keeps the
        two surfaces reading alike. */}
      {health ? null : (
        <div className="flex justify-end">
          <SegmentedControl
            value={grouping}
            onChange={(value: string) =>
              setGrouping(value as ConnectionGrouping)
            }
            options={GROUPING_OPTIONS}
          />
        </div>
      )}

      {clientsTruncated && (
        <p className="text-muted-foreground text-sm">
          Showing the first {clients.length} agents. Later pages were not
          loaded, so search and the agent grouping cover only these.
        </p>
      )}

      {listBody}

      {hasNextPage && (
        <div className="flex justify-center">
          <Button
            variant="tertiary"
            size="sm"
            disabled={isFetchingNextPage}
            onClick={() => void fetchNextPage()}
          >
            {isFetchingNextPage ? "Loading…" : "Load more"}
          </Button>
        </div>
      )}
    </div>
  );
}
