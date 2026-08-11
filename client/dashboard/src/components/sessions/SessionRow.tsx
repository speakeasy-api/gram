import { useState } from "react";
import type { UserSession } from "@gram/client/models/components/usersession.js";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/ContextMenu";
import { MoreActions } from "@/components/ui/MoreActions";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import {
  sessionStatus,
  sessionTimeLabel,
  subjectLabel,
  STATUS_PRESENTATION,
} from "@/lib/user-session-status";
import { ClientSourceBadge } from "./ClientSourceBadge";
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
  const project = useProject();
  const status = sessionStatus(session);
  // Revoke is a write mutation the backend gates on project:write for THIS
  // project. hasScope without a resource id is existential across every
  // project the user holds grants in (ListGrants resolves principals per
  // organization), so an unscoped check would show Revoke to someone who is
  // read-only here but a writer elsewhere, and hand them a 403. Mirrors
  // pages/org/UserSessions.tsx, which scopes the same check by project id.
  const canRevoke =
    status === "active" && hasScope("project:write", project.id);

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
      {session.clientName && <ClientSourceBadge client={session} />}
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
