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
import { ResourceListPage, TabbedPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { ConnectionsList } from "@/components/connections/ConnectionsList";
import {
  CONNECTION_GROUPING_LABELS,
  type ConnectionGrouping,
} from "@/components/connections/groupConnections";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { subjectLabel } from "@/lib/user-session-status";
import { useUserSessionFacets } from "@gram/client/react-query/userSessionFacets.js";
import { useUserSessionsInfinite } from "@gram/client/react-query/userSessions.js";
import type { ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import {
  MCP_SESSIONS_DESCRIPTION,
  MCP_SESSIONS_TITLE,
  useMcpSessionsTabs,
} from "./mcpSessionsTabs";
import { RemoteSessionRefreshPolicySetting } from "./RemoteSessionRefreshPolicySetting";

const USER_SESSION_FILTERS = defineFilters([
  // Unpinned: the list already splits itself into active and inactive, so a
  // status chip sat in the bar restating the layout. Still reachable under
  // "More filters" for the narrower cuts the split does not make — expired
  // versus revoked — and it re-pins itself as a chip once set.
  { id: "status", label: "Status", kind: "select" },
  {
    id: "issuerId",
    label: "MCP server",
    kind: "select",
    pinned: true,
    // `allLabelFor` lowercases the label to pluralize it, which turns MCP into
    // mcp. Fine for ordinary nouns, wrong for an acronym.
    allLabel: "All MCP servers",
  },
  // Unpinned to keep the bar on one line. Project and MCP server are the two
  // axes this page is read along; narrowing to one person is a follow-up
  // question, and the chip appears the moment it is answered.
  // "User", not "Identity": the facets endpoint lists only `user:` subjects,
  // so the wider label offered agents and keys the filter cannot select.
  { id: "subjectUrn", label: "User", kind: "select" },
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
  "Connections Speakeasy brokers for this project. Revoking ends current sessions immediately, but clients can authenticate and reconnect. A killswitch is a separate action that blocks matching MCP tool calls without ending sessions; revocation never creates or lifts one.";

export default function UserSessions(): JSX.Element {
  const project = useProject();
  const tabbed =
    useFeatureFlag(FEATURE_FLAGS.userSessionIssuerDashboard).status ===
    "enabled";
  const tabs = useMcpSessionsTabs();

  if (!tabbed) {
    return (
      <ResourceListPage
        scope="project:read"
        resourceId={project.id}
        title={MCP_SESSIONS_TITLE}
        description={CONNECTIONS_DESCRIPTION}
      >
        <div className="space-y-8">
          {/* `Page.Section` stacks two `mb-6`s under the description, which reads
              as a dropped card when what follows is a surface rather than a
              toolbar. Pulled back locally rather than changing the shared header,
              which every list page is spaced against. */}
          <div className="-mt-6 space-y-4">
            <RemoteSessionRefreshPolicySetting />
          </div>
          <Connections key={project.id} />
        </div>
      </ResourceListPage>
    );
  }

  // Behind gram-user-session-issuer-dashboard: the connections are the
  // Overview tab, and the refresh policy moves to Authorization Servers.
  return (
    <TabbedPage
      title={MCP_SESSIONS_TITLE}
      description={MCP_SESSIONS_DESCRIPTION}
      tabs={tabs}
      activeTab="overview"
    >
      <RequireScope scope="project:read" resourceId={project.id} level="page">
        <div className="space-y-6">
          <p className="text-muted-foreground max-w-3xl text-sm">
            {CONNECTIONS_DESCRIPTION}
          </p>
          <Connections key={project.id} />
        </div>
      </RequireScope>
    </TabbedPage>
  );
}

function Connections(): JSX.Element {
  const project = useProject();
  const { hasScope } = useRBAC();
  const filters = useFilterState(USER_SESSION_FILTERS);
  const projectSlug = project.slug;
  const canRevoke = hasScope("project:write", project.id);
  const [searchQuery, setSearchQuery] = useState("");
  const [grouping, setGrouping] = useState<ConnectionGrouping>("subject");

  const handleFilterChange = (id: string, value: FilterValue) => {
    filters.setValue(id as keyof typeof filters.values, value as never);
  };

  const { data: facets } = useUserSessionFacets({ gramProject: projectSlug });

  const optionsById: OptionsById = useMemo(
    () => ({
      status: STATUS_TOOLBAR_OPTIONS,
      issuerId: (facets?.servers ?? []).map((s) => ({
        value: s.value,
        label: s.displayName,
      })),
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
    userSessionIssuerId: filters.values.issuerId ?? undefined,
  });
  const unresolvedSessions = useMemo(
    () => data?.pages.flatMap((p) => p.result.items) ?? [],
    [data],
  );

  const sessions = useSessionAgents(unresolvedSessions);

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

  let listBody: JSX.Element;
  if (isPending) {
    listBody = (
      <div className="space-y-2">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="h-20 w-full" />
        ))}
      </div>
    );
  } else if (isError && sessions.length === 0) {
    listBody = (
      <div className="flex items-center justify-between gap-3">
        <p className="text-destructive text-sm">
          Couldn&apos;t load connections.
        </p>
        <Button variant="tertiary" size="sm" onClick={() => void refetch()}>
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
  } else if (sessions.length === 0) {
    listBody = <MCPSessionsEmptyState />;
  } else if (filteredSessions.length === 0) {
    listBody = (
      <p className="text-muted-foreground text-sm">
        No connections match your search
      </p>
    );
  } else {
    listBody = (
      <ConnectionsList
        sessions={filteredSessions}
        grouping={grouping}
        canRevoke={canRevoke}
        onRevoked={() => void refetch()}
        killswitchContext={{ capabilityKey: "mcp_tool_calls" }}
        project={project}
      />
    );
  }

  return (
    <div className="space-y-4">
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={searchQuery}
          onChange={setSearchQuery}
          debounceMs={150}
          placeholder="Search connections"
        />
        <Page.Toolbar.Filters
          schema={USER_SESSION_FILTERS}
          values={filters.values}
          optionsById={optionsById}
          onChange={handleFilterChange}
          onClear={filters.clearValue as (id: string) => void}
          onClearAll={filters.clearAll}
        />
        <Page.Toolbar.Refresh
          onRefresh={() => void refetch()}
          isRefreshing={isFetching}
        />
      </Page.Toolbar>

      {/* Outside the bar: the toolbar narrows which connections are listed,
        while grouping re-cuts the ones that survived into a different
        shape. Sitting it directly above the table it restructures — and
        matching where the MCP server tab puts the same control — keeps the
        two surfaces reading alike. */}
      <div className="flex justify-end">
        <SegmentedControl
          value={grouping}
          onChange={(value: string) => setGrouping(value as ConnectionGrouping)}
          options={GROUPING_OPTIONS}
        />
      </div>

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
