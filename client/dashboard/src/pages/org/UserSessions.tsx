import { useDeferredValue, useMemo, useState } from "react";
import { Navigate, useSearchParams } from "react-router";
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
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { subjectLabel } from "@/lib/user-session-status";
import { useUserSessionFacets } from "@gram/client/react-query/userSessionFacets.js";
import { useUserSessionsInfinite } from "@gram/client/react-query/userSessions.js";
import type { QueryParamStatus as ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import { ConsentToolFilteringSetting } from "./ConsentToolFilteringSetting";
import { RemoteSessionRefreshPolicySetting } from "./RemoteSessionRefreshPolicySetting";

const USER_SESSION_FILTERS = defineFilters([
  // Project leads: it re-scopes every other filter's options rather than
  // narrowing the same set, so it reads first in the bar.
  // `required`: the query cannot run without a project, so the chip offers no
  // × — clearing it would resolve straight back to the working project and read
  // as a broken button.
  {
    id: "project",
    label: "Project",
    kind: "select",
    pinned: true,
    required: true,
  },
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

  const filters = useFilterState(USER_SESSION_FILTERS);
  const [, setSearchParams] = useSearchParams();

  // A select filter's empty value is null, but the query always needs a project,
  // so an unset filter means "the one I'm working in" rather than "all". The
  // resolved slug is fed back into the chip's value so it names a real project
  // instead of falling back to an "All projects" that cannot exist.
  const projectSlug = filters.values.project ?? project.slug;
  const filterValues = useMemo(
    () => ({ ...filters.values, project: projectSlug }),
    [filters.values, projectSlug],
  );

  // Revoke is a write mutation (backend requires project:write). Scope the check
  // to the *selected* project — a user with project:write on one project must
  // not see revoke affordances after switching to another (they'd only fail at
  // mutation time).
  const selectedProjectId = projects.find((p) => p.slug === projectSlug)?.id;
  const canRevoke =
    !!selectedProjectId && hasScope("project:write", selectedProjectId);
  const [searchQuery, setSearchQuery] = useState("");
  const [grouping, setGrouping] = useState<ConnectionGrouping>("subject");

  // Server and user options are facets of one project, so a value chosen in one
  // means nothing in another and has to go when the project does. Written in a
  // single navigation rather than a setValue followed by clears: react-router
  // reads a memoized snapshot per render, so chained updates clobber each other
  // and only the last would survive.
  const handleFilterChange = (id: string, value: FilterValue) => {
    if (id !== "project") {
      filters.setValue(id as keyof typeof filters.values, value as never);
      return;
    }

    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (typeof value === "string" && value) {
          next.set("project", value);
        } else {
          next.delete("project");
        }
        next.delete("status");
        next.delete("issuerId");
        next.delete("subjectUrn");
        return next;
      },
      { replace: true },
    );
    setSearchQuery("");
  };

  const { data: facets } = useUserSessionFacets({ gramProject: projectSlug });

  const optionsById: OptionsById = useMemo(
    () => ({
      project: projects.map((p) => ({ value: p.slug, label: p.slug })),
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
    [facets, projects],
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
        project={
          selectedProjectId
            ? { slug: projectSlug, id: selectedProjectId }
            : undefined
        }
      />
    );
  }

  return (
    <ResourceListPage
      scope="org:read"
      title="MCP Sessions"
      description="Every connection Gram brokers: what an agent connects through, the MCP server it reaches, and the upstream provider Gram holds credentials for on that person's behalf. Revoke a connection to immediately cut off access."
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
              values={filterValues}
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
