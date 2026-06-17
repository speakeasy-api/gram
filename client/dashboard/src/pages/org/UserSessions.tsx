import { useMemo, useState } from "react";
import { FacetSelect } from "@/components/auditlogs/feed";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
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

const STATUS_OPTIONS = ["all", "active", "expired", "revoked"] as const;
type StatusFilter = (typeof STATUS_OPTIONS)[number];

const STATUS_FILTER_OPTIONS = STATUS_OPTIONS.filter((o) => o !== "all").map(
  (o) => ({ value: o, displayName: o[0]!.toUpperCase() + o.slice(1) }),
);

export default function UserSessions(): JSX.Element {
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
  const [status, setStatus] = useState<StatusFilter>("all");
  const [clientId, setClientId] = useState("all");
  const [subjectUrn, setSubjectUrn] = useState("all");
  const [issuerId, setIssuerId] = useState("all");

  // Reset project-specific facet filters when switching projects so a stale
  // client/user/server selection from one project isn't submitted to another.
  const handleProjectChange = (slug: string) => {
    setProjectSlug(slug);
    setClientId("all");
    setSubjectUrn("all");
    setIssuerId("all");
  };

  const { data: facets } = useUserSessionFacets({ gramProject: projectSlug });

  const {
    data,
    isPending,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    refetch,
  } = useUserSessionsInfinite({
    gramProject: projectSlug,
    status:
      status === "all"
        ? undefined
        : (status as ListUserSessionsQueryParamStatus),
    clientId: clientId === "all" ? undefined : clientId,
    subjectUrn: subjectUrn === "all" ? undefined : subjectUrn,
    userSessionIssuerId: issuerId === "all" ? undefined : issuerId,
  });
  const sessions = data?.pages.flatMap((p) => p.result.items) ?? [];

  return (
    <Page.Section>
      <Page.Section.Title>User Sessions</Page.Section.Title>
      <Page.Section.Description>
        Sessions clients hold into this project&apos;s MCP servers, established
        via OAuth. Revoke a session to immediately cut off access.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="space-y-4">
          <div className="flex flex-wrap gap-2">
            <div className="flex flex-col gap-1.5">
              <Type small muted>
                Project
              </Type>
              <Select value={projectSlug} onValueChange={handleProjectChange}>
                <SelectTrigger
                  size="sm"
                  className="bg-background min-w-[220px]"
                >
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
            <FacetSelect
              label="Status"
              value={status}
              onValueChange={(v) => setStatus(v as StatusFilter)}
              placeholder="All statuses"
              allLabel="All statuses"
              options={STATUS_FILTER_OPTIONS}
            />
            <FacetSelect
              label="MCP server"
              value={issuerId}
              onValueChange={setIssuerId}
              placeholder="All servers"
              allLabel="All servers"
              options={facets?.servers ?? []}
            />
            <FacetSelect
              label="OAuth Clients"
              value={clientId}
              onValueChange={setClientId}
              placeholder="All OAuth clients"
              allLabel="All OAuth clients"
              options={facets?.clients ?? []}
            />
            <FacetSelect
              label="User"
              value={subjectUrn}
              onValueChange={setSubjectUrn}
              placeholder="All users"
              allLabel="All users"
              options={facets?.users ?? []}
            />
          </div>

          {isPending ? (
            <div className="space-y-2">
              {Array.from({ length: 8 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
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
