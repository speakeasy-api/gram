import { useState } from "react";
import type { UserSession } from "@gram/client/models/components";

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
}: {
  session: UserSession;
  onRevoked: () => void;
}): JSX.Element {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const status = sessionStatus(session);
  const canRevoke = status === "active";

  return (
    <>
      <DotRow>
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

      <RevokeSessionDialog
        session={session}
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        onRevoked={onRevoked}
      />
    </>
  );
}
