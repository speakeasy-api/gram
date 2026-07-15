import { useState } from "react";
import type { UserSession } from "@gram/client/models/components/usersession.js";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { MoreActions } from "@/components/ui/more-actions";
import { StatusDot } from "@/components/ui/status-dot";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
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
      <StatusDot tone={STATUS_PRESENTATION[status].badgeVariant} />
      <div className="min-w-0 flex-1">
        <Type as="p" small className="truncate font-medium">
          {subjectLabel(session)}
        </Type>
        <Type as="p" muted small className="truncate text-xs">
          {session.clientName ? `${session.clientName} · ` : ""}gated by{" "}
          {session.issuerSlug}
        </Type>
      </div>
      <SessionStatusBadge session={session} />
      <Type as="span" muted small className="shrink-0 text-xs">
        {sessionTimeLabel(session)}
      </Type>
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
