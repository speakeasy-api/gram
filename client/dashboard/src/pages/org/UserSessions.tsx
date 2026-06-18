import { useMemo, useState } from "react";
import { Navigate } from "react-router";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useTelemetry } from "@/contexts/Telemetry";
import { useOrgRoutes } from "@/routes";
import { SessionTableRow } from "@/components/sessions/SessionTableRow";
import { Button } from "@/components/ui/button";
import { DotTable } from "@/components/ui/dot-table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useOrganization, useProject } from "@/contexts/Auth";
import {
  useUserSessionFacets,
  useUserSessionsInfinite,
} from "@gram/client/react-query";
import type { ListUserSessionsQueryParamStatus } from "@gram/client/models/operations";

const USER_SESSION_FILTERS = defineFilters([
  { id: "status", label: "Status", kind: "select", pinned: true },
  { id: "issuerId", label: "MCP server", kind: "select", pinned: true },
  { id: "clientId", label: "OAuth Client", kind: "select", pinned: true },
  { id: "subjectUrn", label: "User", kind: "select", pinned: true },
]);

const STATUS_TOOLBAR_OPTIONS = [
  { value: "active", label: "Active" },
  { value: "expired", label: "Expired" },
  { value: "revoked", label: "Revoked" },
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

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:read" level="page">
          <UserSessionsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function UserSessionsInner(): JSX.Element {
  const project = useProject();
  const organization = useOrganization();

  const projects = useMemo(
    () =>
      [...organization.projects].sort((a, b) => a.slug.localeCompare(b.slug)),
    [organization.projects],
  );

  const [projectSlug, setProjectSlug] = useState<string>(project.slug);
  const filters = useFilterState(USER_SESSION_FILTERS);

  // Reset facet filters when switching projects so a stale selection from one
  // project isn't submitted to another.
  const handleProjectChange = (slug: string) => {
    setProjectSlug(slug);
    filters.clearAll();
  };

  const { data: facets } = useUserSessionFacets({ gramProject: projectSlug });

  const optionsById: OptionsById = useMemo(
    () => ({
      status: STATUS_TOOLBAR_OPTIONS,
      issuerId: (facets?.servers ?? []).map((s) => ({
        value: s.value,
        label: s.displayName,
      })),
      clientId: (facets?.clients ?? []).map((c) => ({
        value: c.value,
        label: c.displayName,
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
    refetch,
  } = useUserSessionsInfinite({
    gramProject: projectSlug,
    status: (filters.values.status ?? undefined) as
      | ListUserSessionsQueryParamStatus
      | undefined,
    clientId: filters.values.clientId ?? undefined,
    subjectUrn: filters.values.subjectUrn ?? undefined,
    userSessionIssuerId: filters.values.issuerId ?? undefined,
  });
  const sessions = data?.pages.flatMap((p) => p.result.items) ?? [];

  return (
    <Page.Section>
      <Page.Section.Title>MCP Connections</Page.Section.Title>
      <Page.Section.Description>
        Sessions clients hold into this project&apos;s MCP servers, established
        via OAuth. Revoke a session to immediately cut off access.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="space-y-4">
          <div className="flex flex-col gap-1.5">
            <Type small muted>
              Project
            </Type>
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
              {sessions.length} session{sessions.length === 1 ? "" : "s"}
            </Page.Toolbar.Count>
          </Page.Toolbar>

          {isPending ? (
            <div className="space-y-2">
              {Array.from({ length: 8 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : isError ? (
            <div className="flex items-center justify-between gap-3">
              <p className="text-destructive text-sm">
                Couldn&apos;t load sessions.
              </p>
              <Button variant="ghost" size="sm" onClick={() => void refetch()}>
                Retry
              </Button>
            </div>
          ) : sessions.length === 0 ? (
            <p className="text-muted-foreground text-sm">No sessions found</p>
          ) : (
            <DotTable
              headers={[
                { label: "Subject" },
                { label: "OAuth Client" },
                { label: "MCP server" },
                { label: "Status" },
                { label: "Expires" },
                { label: "", className: "w-10" },
              ]}
            >
              {sessions.map((s) => (
                <SessionTableRow
                  key={s.id}
                  session={s}
                  onRevoked={() => void refetch()}
                />
              ))}
            </DotTable>
          )}

          {hasNextPage && (
            <div className="flex justify-center">
              <Button
                variant="ghost"
                size="sm"
                disabled={isFetchingNextPage}
                onClick={() => void fetchNextPage()}
              >
                {isFetchingNextPage ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
