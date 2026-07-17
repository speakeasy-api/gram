import { useState } from "react";
import type { UserSession } from "@gram/client/models/components/usersession.js";

import { Checkbox } from "@/components/ui/checkbox";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { DotRow } from "@/components/ui/dot-row";
import { MoreActions } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";
import {
  sessionStatus,
  sessionTimeLabel,
  subjectLabel,
} from "@/lib/user-session-status";
import { SessionStatusBadge } from "./SessionStatusBadge";
import { RevokeSessionDialog } from "./RevokeSessionDialog";

export function SessionTableRow({
  session,
  onRevoked,
  canRevokeInProject = false,
  selectable = false,
  selected = false,
  onSelectedChange,
}: {
  session: UserSession;
  onRevoked: () => void;
  /**
   * Whether the user holds project:write on this row's project (project-scoped,
   * resolved by the parent). Combined with the row's status below.
   */
  canRevokeInProject?: boolean;
  /** Render the leading selection cell so columns align with the table header. */
  selectable?: boolean;
  selected?: boolean;
  onSelectedChange?: (checked: boolean) => void;
}): JSX.Element {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const status = sessionStatus(session);
  // Only active sessions can be revoked, and only when the user can write to
  // this project (hide the affordance rather than 403 later).
  const canRevoke = status === "active" && canRevokeInProject;

  const row = (
    <DotRow>
      {selectable && (
        <td className="w-10 px-3 py-3">
          {canRevoke && (
            <div className="flex" onClick={(e) => e.stopPropagation()}>
              <Checkbox
                checked={selected}
                onCheckedChange={(c) => onSelectedChange?.(c === true)}
                aria-label={`Select session for ${subjectLabel(session)}`}
              />
            </div>
          )}
        </td>
      )}

      {/* Subject */}
      <td className="px-3 py-3">
        <Type
          variant="subheading"
          as="div"
          className="truncate text-sm"
          title={subjectLabel(session)}
        >
          {subjectLabel(session)}
        </Type>
      </td>

      {/* Client */}
      <td className="px-3 py-3">
        <Type small muted>
          {session.clientName ?? "—"}
        </Type>
      </td>

      {/* MCP server */}
      <td className="px-3 py-3">
        <Type small muted>
          {session.issuerSlug}
        </Type>
      </td>

      {/* Status */}
      <td className="px-3 py-3">
        <SessionStatusBadge session={session} />
      </td>

      {/* Expires / Revoked */}
      <td className="px-3 py-3">
        <Type small muted>
          {sessionTimeLabel(session)}
        </Type>
      </td>

      {/* Actions */}
      <td className="px-3 py-3">
        {canRevoke && (
          <div
            className="flex justify-end"
            onClick={(e) => e.stopPropagation()}
          >
            <MoreActions
              actions={[
                {
                  label: "Revoke",
                  icon: "trash" as const,
                  destructive: true,
                  onClick: () => setConfirmOpen(true),
                },
              ]}
            />
          </div>
        )}
      </td>
    </DotRow>
  );

  return (
    <>
      {canRevoke ? (
        <ContextMenu>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
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
        row
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
