import { useState } from "react";
import { format, formatDistanceToNow } from "date-fns";
import type { UserSession } from "@gram/client/models/components";
import { useRevokeUserSessionMutation } from "@gram/client/react-query";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { Dialog } from "@/components/ui/dialog";
import { MoreActions } from "@/components/ui/more-actions";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  sessionStatus,
  subjectLabel,
  STATUS_PRESENTATION,
} from "@/lib/user-session-status";
import { SessionStatusBadge } from "./SessionStatusBadge";

export function SessionRow({
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

  const doRevoke = () =>
    revoke.mutate(
      { request: { id: session.id } },
      {
        onSuccess: () => {
          setConfirmOpen(false);
          onRevoked();
        },
      },
    );

  const rowContent = (
    <li className="flex items-center gap-3 px-3 py-2">
      <span
        className={cn(
          "size-2 shrink-0 rounded-full",
          STATUS_PRESENTATION[status].dotClass,
        )}
      />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{subjectLabel(session)}</p>
        <p className="text-muted-foreground truncate text-xs">
          {session.clientName ? `${session.clientName} · ` : ""}gated by{" "}
          {session.issuerSlug}
        </p>
      </div>
      <SessionStatusBadge session={session} />
      <span className="text-muted-foreground shrink-0 text-xs">
        {status === "revoked" && session.revokedAt
          ? `revoked ${format(new Date(session.revokedAt), "PP")}`
          : `expires ${formatDistanceToNow(new Date(session.expiresAt), { addSuffix: true })}`}
      </span>
      {canRevoke && (
        <MoreActions
          actions={[
            {
              label: "Revoke",
              destructive: true,
              onClick: () => setConfirmOpen(true),
            },
          ]}
        />
      )}
    </li>
  );

  return (
    <>
      {canRevoke ? (
        <ContextMenu>
          <ContextMenuTrigger asChild>{rowContent}</ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem
              variant="destructive"
              onSelect={() => setConfirmOpen(true)}
            >
              Revoke session
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : (
        rowContent
      )}
      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke session?</Dialog.Title>
            <Dialog.Description>
              This immediately invalidates the session for{" "}
              {subjectLabel(session)}. The client will need to re-authenticate.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button variant="ghost" onClick={() => setConfirmOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={revoke.isPending}
              onClick={doRevoke}
            >
              {revoke.isPending ? "Revoking…" : "Revoke"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
