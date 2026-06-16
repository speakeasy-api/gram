import { useState } from "react";
import { FacetSelect } from "@/components/auditlogs/feed";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { SessionRow } from "@/components/sessions/SessionRow";
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
        <RequireScope scope="project:read" level="page">
          <UserSessionsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function UserSessionsInner(): JSX.Element {
  const [status, setStatus] = useState<StatusFilter>("all");
  const [clientId, setClientId] = useState("all");
  const [subjectUrn, setSubjectUrn] = useState("all");
  const [issuerId, setIssuerId] = useState("all");

  const { data: facets } = useUserSessionFacets({});

  const {
    data,
    isPending,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    refetch,
  } = useUserSessionsInfinite({
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
    <div className="space-y-4">
      <div className="flex flex-wrap gap-2">
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
          label="Client"
          value={clientId}
          onValueChange={setClientId}
          placeholder="All clients"
          allLabel="All clients"
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
        <ul className="divide-border divide-y rounded-md border">
          {sessions.map((s) => (
            <SessionRow
              key={s.id}
              session={s}
              onRevoked={() => void refetch()}
            />
          ))}
        </ul>
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
  );
}
