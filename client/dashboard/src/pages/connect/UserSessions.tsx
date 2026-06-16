import { useState } from "react";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { SessionRow } from "@/components/sessions/SessionRow";
import { useUserSessionsInfinite } from "@gram/client/react-query";
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
  const {
    data,
    isPending,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    refetch,
  } = useUserSessionsInfinite({
    status: status as ListUserSessionsQueryParamStatus,
  });
  const sessions = data?.pages.flatMap((p) => p.result.items) ?? [];

  return (
    <div className="space-y-4">
      <div className="flex gap-2">
        {STATUS_OPTIONS.map((opt) => (
          <Button
            key={opt}
            variant={status === opt ? "secondary" : "ghost"}
            size="sm"
            onClick={() => setStatus(opt)}
          >
            {opt[0]!.toUpperCase() + opt.slice(1)}
          </Button>
        ))}
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
