import { useDeferredValue, useMemo, useState } from "react";
import { Navigate } from "react-router";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { ResourceListPage } from "@/components/page-templates";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { ConnectionsList } from "@/components/connections/ConnectionsList";
import {
  CONNECTION_GROUPING_LABELS,
  type ConnectionGrouping,
} from "@/components/connections/groupConnections";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { subjectLabel } from "@/lib/user-session-status";
import { useUserSessionFacets } from "@gram/client/react-query/userSessionFacets.js";
import { useUserSessionsInfinite } from "@gram/client/react-query/userSessions.js";
import type { QueryParamStatus as ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import { ConsentToolFilteringSetting } from "./ConsentToolFilteringSetting";
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

export default function UserSessions(): JSX.Element {
  const telemetry = useTelemetry();
  const routes = useRoutes();

  // Gated behind the `user-sessions-dashboard` PostHog flag (internal rollout).
  // Redirect direct-URL access when the flag has resolved to disabled; while it
  // is still loading (undefined) we render and let RBAC guard the data.
  if (telemetry.isFeatureEnabled("user-sessions-dashboard") === false) {
    return <Navigate to={routes.home.href()} replace />;
  }

  return <UserSessionsInner />;
}

function UserSessionsInner(): JSX.Element {
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
  const sessions = useMemo(
    () => data?.pages.flatMap((p) => p.result.items) ?? [],
    [data],
  );

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
  } else if (sessions.length === 0) {
    listBody = (
      <div className="flex flex-col items-center justify-center border border-dashed px-8 py-16">
        <Text variant="subheading" className="mb-1">
          No connections yet
        </Text>
        <Text small muted className="max-w-md text-center">
          Connections agents establish with your MCP servers will appear here.
        </Text>
      </div>
    );
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
    <ResourceListPage
      scope="org:read"
      title="MCP Sessions"
      description="Connections Gram brokers for this project. Revoking ends current sessions immediately, but clients can authenticate and reconnect. A killswitch is a separate action that blocks matching MCP tool calls without ending sessions; revocation never creates or lifts one."
    >
      <div className="space-y-8">
        {/* `Page.Section` stacks two `mb-6`s under the description, which reads
            as a dropped card when what follows is a surface rather than a
            toolbar. Pulled back locally rather than changing the shared header,
            which every list page is spaced against. */}
        <div className="-mt-6 space-y-4">
          <RemoteSessionRefreshPolicySetting />

          <ConsentToolFilteringSetting />
        </div>

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
              onChange={(value: string) =>
                setGrouping(value as ConnectionGrouping)
              }
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
      </div>
    </ResourceListPage>
  );
}
