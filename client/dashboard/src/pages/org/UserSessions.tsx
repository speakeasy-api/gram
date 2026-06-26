import { useDeferredValue, useMemo, useState } from "react";
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
import { RevokeSessionsDialog } from "@/components/sessions/RevokeSessionsDialog";
import { SessionTableRow } from "@/components/sessions/SessionTableRow";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import { useRBAC } from "@/hooks/useRBAC";
import { sessionStatus, subjectLabel } from "@/lib/user-session-status";
import {
  useUserSessionFacets,
  useUserSessionsInfinite,
} from "@gram/client/react-query";
import type { ListUserSessionsQueryParamStatus } from "@gram/client/models/operations";

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

// Tri-state for the select-all header checkbox.
function selectAllState(
  selectedCount: number,
  totalCount: number,
): boolean | "indeterminate" {
  if (selectedCount === 0) return false;
  if (selectedCount === totalCount) return true;
  return "indeterminate";
}

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

  const { hasScope } = useRBAC();

  const [projectSlug, setProjectSlug] = useState<string>(project.slug);
  const filters = useFilterState(USER_SESSION_FILTERS);

  // Revoke is a write mutation (backend requires project:write). Scope the check
  // to the *selected* project — a user with project:write on one project must
  // not see revoke affordances after switching to another (they'd only fail at
  // mutation time). Drives both the bulk selection and the per-row affordances.
  const selectedProjectId = projects.find((p) => p.slug === projectSlug)?.id;
  const canRevoke =
    !!selectedProjectId && hasScope("project:write", selectedProjectId);
  const [searchQuery, setSearchQuery] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulkConfirmOpen, setBulkConfirmOpen] = useState(false);

  // Reset facet filters and selection when switching projects so a stale
  // selection from one project isn't submitted to another.
  const handleProjectChange = (slug: string) => {
    setProjectSlug(slug);
    filters.clearAll();
    setSearchQuery("");
    setSelected(new Set());
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

  // Search filters the loaded rows client-side (subject / client / server),
  // matching the loaded-count semantics shown in the toolbar. Deferred so the
  // input stays responsive while the list re-filters.
  const deferredSearch = useDeferredValue(searchQuery);
  const filteredSessions = useMemo(() => {
    const q = deferredSearch.trim().toLowerCase();
    if (!q) return sessions;
    return sessions.filter(
      (s) =>
        subjectLabel(s).toLowerCase().includes(q) ||
        (s.clientName ?? "").toLowerCase().includes(q) ||
        s.issuerSlug.toLowerCase().includes(q),
    );
  }, [sessions, deferredSearch]);

  // Only active sessions can be revoked, so selection (and select-all) operates
  // over the active rows currently in view.
  const activeIds = useMemo(
    () =>
      filteredSessions
        .filter((s) => sessionStatus(s) === "active")
        .map((s) => s.id),
    [filteredSessions],
  );
  const selectedIds = activeIds.filter((id) => selected.has(id));
  const selectionEnabled = canRevoke && activeIds.length > 0;

  const toggleOne = (id: string, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const toggleAll = (checked: boolean) => {
    setSelected(checked ? new Set(activeIds) : new Set());
  };

  let listBody: JSX.Element;
  if (isPending) {
    listBody = (
      <div className="space-y-2">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </div>
    );
  } else if (isError && sessions.length === 0) {
    listBody = (
      <div className="flex items-center justify-between gap-3">
        <p className="text-destructive text-sm">Couldn&apos;t load sessions.</p>
        <Button variant="ghost" size="sm" onClick={() => void refetch()}>
          Retry
        </Button>
      </div>
    );
  } else if (sessions.length === 0) {
    listBody = (
      <p className="text-muted-foreground text-sm">No sessions found</p>
    );
  } else if (filteredSessions.length === 0) {
    listBody = (
      <p className="text-muted-foreground text-sm">
        No sessions match your search
      </p>
    );
  } else {
    listBody = (
      <DotTable
        selectionHeader={
          selectionEnabled ? (
            <Checkbox
              checked={selectAllState(selectedIds.length, activeIds.length)}
              onCheckedChange={(c) => toggleAll(c === true)}
              aria-label="Select all active sessions"
            />
          ) : undefined
        }
        headers={[
          { label: "Subject" },
          { label: "OAuth Client" },
          { label: "MCP server" },
          { label: "Status" },
          { label: "Expires" },
          { label: "", className: "w-10" },
        ]}
      >
        {filteredSessions.map((s) => (
          <SessionTableRow
            key={s.id}
            session={s}
            canRevokeInProject={canRevoke}
            onRevoked={() => void refetch()}
            selectable={selectionEnabled}
            selected={selected.has(s.id)}
            onSelectedChange={(checked) => toggleOne(s.id, checked)}
          />
        ))}
      </DotTable>
    );
  }

  return (
    <Page.Section>
      <Page.Section.Title>MCP Connections</Page.Section.Title>
      <Page.Section.Description>
        View and manage active connections agents have established with your MCP
        servers, established via OAuth. Revoke a connection to immediately cut
        off access.
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
            <Page.Toolbar.Search
              value={searchQuery}
              onChange={setSearchQuery}
              debounceMs={150}
              placeholder="Search sessions"
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
              {filteredSessions.length} session
              {filteredSessions.length === 1 ? "" : "s"}
            </Page.Toolbar.Count>
          </Page.Toolbar>

          {selectionEnabled && selectedIds.length > 0 && (
            <div className="border-border bg-muted/30 flex items-center justify-between gap-3 rounded-md border px-3 py-2">
              <Type small>{selectedIds.length} selected</Type>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setSelected(new Set())}
                >
                  Clear
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setBulkConfirmOpen(true)}
                >
                  Revoke {selectedIds.length}
                </Button>
              </div>
            </div>
          )}

          {listBody}

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

        <RevokeSessionsDialog
          sessionIds={selectedIds}
          open={bulkConfirmOpen}
          onOpenChange={setBulkConfirmOpen}
          onRevoked={(succeededIds) => {
            // Clear only the sessions that actually revoked; keep any failures
            // selected so the user can retry them.
            setSelected((prev) => {
              const next = new Set(prev);
              for (const id of succeededIds) next.delete(id);
              return next;
            });
            void refetch();
          }}
        />
      </Page.Section.Body>
    </Page.Section>
  );
}
