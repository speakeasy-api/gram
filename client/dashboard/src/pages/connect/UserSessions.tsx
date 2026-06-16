import { useState } from "react";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { sessionStatus, subjectLabel } from "@/lib/user-session-status";
import {
  useUserSessionsInfinite,
  useRevokeUserSessionMutation,
} from "@gram/client/react-query";
import type { UserSession } from "@gram/client/models/components";
import type { ListUserSessionsQueryParamStatus } from "@gram/client/models/operations";
import { format, formatDistanceToNow } from "date-fns";

const STATUS_OPTIONS = ["all", "active", "expired", "revoked"] as const;
type StatusFilter = (typeof STATUS_OPTIONS)[number];

const STATUS_DOT: Record<string, string> = {
  active: "bg-emerald-500",
  expired: "bg-muted-foreground",
  revoked: "bg-destructive",
};

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

function SessionRow({
  session,
  onRevoked,
}: {
  session: UserSession;
  onRevoked: () => void;
}): JSX.Element {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const revoke = useRevokeUserSessionMutation();
  const status = sessionStatus(session);
  const canRevoke = status === "active";

  return (
    <li className="flex items-center gap-3 px-3 py-2">
      <span
        className={cn("size-2 shrink-0 rounded-full", STATUS_DOT[status])}
      />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{subjectLabel(session)}</p>
        <p className="text-muted-foreground truncate text-xs">
          {session.clientName ? `${session.clientName} · ` : ""}
          gated by {session.issuerSlug}
        </p>
      </div>
      <span className="text-muted-foreground shrink-0 text-xs">
        {status === "revoked" && session.revokedAt
          ? `revoked ${format(new Date(session.revokedAt), "PP")}`
          : `expires ${formatDistanceToNow(new Date(session.expiresAt), { addSuffix: true })}`}
      </span>
      {canRevoke && (
        <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
          <Dialog.Trigger asChild>
            <Button variant="destructive" size="sm">
              Revoke
            </Button>
          </Dialog.Trigger>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Revoke session?</Dialog.Title>
              <Dialog.Description>
                This immediately invalidates the session for{" "}
                {subjectLabel(session)}. The client will need to
                re-authenticate.
              </Dialog.Description>
            </Dialog.Header>
            <Dialog.Footer>
              <Button variant="ghost" onClick={() => setConfirmOpen(false)}>
                Cancel
              </Button>
              <Button
                variant="destructive"
                disabled={revoke.isPending}
                onClick={() =>
                  revoke.mutate(
                    { request: { id: session.id } },
                    {
                      onSuccess: () => {
                        setConfirmOpen(false);
                        onRevoked();
                      },
                    },
                  )
                }
              >
                {revoke.isPending ? "Revoking…" : "Revoke"}
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      )}
    </li>
  );
}
