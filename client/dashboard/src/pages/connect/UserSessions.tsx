import { useState } from "react";
import { FacetSelect } from "@/components/auditlogs/feed";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { SessionRow } from "@/components/sessions/SessionRow";
import { STATUS_PRESENTATION } from "@/lib/user-session-status";
import { cn } from "@/lib/utils";
import {
  useUserSessionFacets,
  useUserSessionsInfinite,
} from "@gram/client/react-query";
import type { ListUserSessionsQueryParamStatus } from "@gram/client/models/operations";

const STATUS_OPTIONS = ["all", "active", "expired", "revoked"] as const;
type StatusFilter = (typeof STATUS_OPTIONS)[number];

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
        {STATUS_OPTIONS.map((opt) => {
          const selected = status === opt;
          const dotClass =
            opt === "all" ? null : STATUS_PRESENTATION[opt].dotClass;
          return (
            <button
              key={opt}
              type="button"
              aria-pressed={selected}
              onClick={() => setStatus(opt)}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors",
                selected
                  ? "border-border bg-secondary text-secondary-foreground"
                  : "text-muted-foreground hover:bg-muted border-transparent",
              )}
            >
              {dotClass && (
                <span className={cn("size-2 rounded-full", dotClass)} />
              )}
              {opt[0]!.toUpperCase() + opt.slice(1)}
            </button>
          );
        })}
      </div>

      <div className="flex flex-wrap gap-2">
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
