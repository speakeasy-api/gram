import { useState } from "react";
import type { UserSession } from "@gram/client/models/components";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { MoreActions } from "@/components/ui/more-actions";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import {
  sessionStatus,
  sessionTimeLabel,
  subjectLabel,
  STATUS_PRESENTATION,
} from "@/lib/user-session-status";
import { SessionStatusBadge } from "./SessionStatusBadge";
import { RevokeSessionDialog } from "./RevokeSessionDialog";

export function SessionRow({
  session,
  onRevoked,
}: {
  session: UserSession;
  onRevoked: () => void;
}): JSX.Element {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const { hasScope } = useRBAC();
  const status = sessionStatus(session);
  // Revoke is a write mutation (backend requires project:write); hide the
  // affordance for read-only users instead of letting them hit a 403.
  const canRevoke = status === "active" && hasScope("project:write");

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
        {sessionTimeLabel(session)}
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
      <RevokeSessionDialog
        session={session}
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        onRevoked={onRevoked}
      />
    </>
  );
}
