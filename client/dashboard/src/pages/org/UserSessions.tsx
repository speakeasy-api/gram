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
import { useOrgRoutes } from "@/routes";
import { ConnectionsList } from "@/components/connections/ConnectionsList";
import {
  CONNECTION_GROUPING_LABELS,
  type ConnectionGrouping,
} from "@/components/connections/groupConnections";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { subjectLabel } from "@/lib/user-session-status";
import { useUserSessionFacets } from "@gram/client/react-query/userSessionFacets.js";
import { useUserSessionsInfinite } from "@gram/client/react-query/userSessions.js";
import type { QueryParamStatus as ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import { RemoteSessionRefreshPolicySetting } from "./RemoteSessionRefreshPolicySetting";

const USER_SESSION_FILTERS = defineFilters([
  { id: "status", label: "Status", kind: "select", pinned: true },
  { id: "issuerId", label: "MCP server", kind: "select", pinned: true },
  { id: "subjectUrn", label: "User", kind: "select", pinned: true },
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
  const orgRoutes = useOrgRoutes();

  // Gated behind the `user-sessions-dashboard` PostHog flag (internal rollout).
  // Redirect direct-URL access when the flag has resolved to disabled; while it
  // is still loading (undefined) we render and let RBAC guard the data.
  if (telemetry.isFeatureEnabled("user-sessions-dashboard") === false) {
    return <Navigate to={orgRoutes.home.href()} replace />;
  }

  return <UserSessionsInner />;
}

function UserSessionsInner(): JSX.Element {
  const project = useProject();
  const organization = useOrganization();

  const projects = useMemo(
    () =>
      [...organization.projects].sort((a, b) => a.slug.localeCompare(b.slug)),
    [organization.projects],
  );

  const { hasScope } = useRBAC();

  const [projectSlug, setProjectSlug] = useState<string>(project.slug);
  const filters = useFilterState(USER_SESSION_FILTERS);

  // Revoke is a write mutation (backend requires project:write). Scope the check
  // to the *selected* project — a user with project:write on one project must
  // not see revoke affordances after switching to another (they'd only fail at
  // mutation time).
  const selectedProjectId = projects.find((p) => p.slug === projectSlug)?.id;
  const canRevoke =
    !!selectedProjectId && hasScope("project:write", selectedProjectId);
  const [searchQuery, setSearchQuery] = useState("");
  const [grouping, setGrouping] = useState<ConnectionGrouping>("subject");

  // Reset facet filters when switching projects so a stale filter from one
  // project isn't submitted to another.
  const handleProjectChange = (slug: string) => {
    setProjectSlug(slug);
    filters.clearAll();
    setSearchQuery("");
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
      />
    );
  }

  return (
    <ResourceListPage
      scope="org:read"
      title="MCP Connections"
      description="Every connection Gram brokers: what an agent connects through, the MCP server it reaches, and the upstream provider Gram holds credentials for on that person's behalf. Revoke a connection to immediately cut off access."
    >
      <div className="space-y-4">
        <RemoteSessionRefreshPolicySetting />

        <div className="flex flex-col gap-1.5">
          <Text small muted>
            Project
          </Text>
          <Select value={projectSlug} onValueChange={handleProjectChange}>
            <SelectTrigger size="sm" className="bg-background w-[260px]">
              <SelectValue placeholder="Select project" />
            </SelectTrigger>
            <SelectContent>
              {projects.map((p) => (
                <SelectItem key={p.slug} value={p.slug}>
                  {p.slug}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

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
            onChange={
              filters.setValue as (id: string, value: FilterValue) => void
            }
            onClear={filters.clearValue as (id: string) => void}
            onClearAll={filters.clearAll}
          />
          <Page.Toolbar.Count>
            {filteredSessions.length} connection
            {filteredSessions.length === 1 ? "" : "s"}
          </Page.Toolbar.Count>
          <Page.Toolbar.Actions>
            <SegmentedControl
              value={grouping}
              onChange={(value: string) =>
                setGrouping(value as ConnectionGrouping)
              }
              options={GROUPING_OPTIONS}
            />
          </Page.Toolbar.Actions>
          <Page.Toolbar.Refresh
            onRefresh={() => void refetch()}
            isRefreshing={isFetching}
          />
        </Page.Toolbar>

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
    </ResourceListPage>
  );
}
