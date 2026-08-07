import { format } from "date-fns";
import { useState } from "react";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/ContextMenu";
import { DotRow } from "@/components/ui/DotRow";
import { MoreActions } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { clientDocumentOrigin } from "@/lib/user-session-client-source";
import { ClientSourceBadge } from "./ClientSourceBadge";
import { RevokeClientDialog } from "./RevokeClientDialog";

export function ClientTableRow({
  client,
  onRevoked,
  onViewSessions,
}: {
  client: UserSessionClient;
  onRevoked: () => void;
  onViewSessions: (client: UserSessionClient) => void;
}): JSX.Element {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const { hasScope } = useRBAC();
  const project = useProject();
  // Revoke is a write mutation the backend gates on project:write for THIS
  // project. hasScope without a resource id is existential across every
  // project the user holds grants in (ListGrants resolves principals per
  // organization), so an unscoped check would show Revoke to someone who is
  // read-only here but a writer elsewhere, and hand them a 403. Mirrors
  // pages/org/UserSessions.tsx, which scopes the same check by project id.
  const canRevoke = hasScope("project:write", project.id);
  const origin = clientDocumentOrigin(client);
  const secondaryLabel = origin ?? client.clientId;

  const actions = [
    {
      label: "View sessions",
      onClick: () => onViewSessions(client),
    },
    ...(canRevoke
      ? [
          {
            label: "Revoke",
            icon: "trash" as const,
            destructive: true,
            onClick: () => setConfirmOpen(true),
          },
        ]
      : []),
  ];

  const row = (
    <DotRow>
      {/* Client. client_name is chosen by the client and verified by nobody,
          so a CIMD row is labelled underneath with its document origin, which
          is the part of its identity it cannot forge. A DCR row has no such
          origin, so it shows the client_id Gram minted for it instead --
          useful for correlating against logs, and human-sized (a CIMD
          client_id is the document URL, up to ~2KB). */}
      <td className="px-3 py-3">
        <Text
          variant="subheading"
          as="div"
          className="truncate text-sm"
          title={client.clientName}
        >
          {client.clientName}
        </Text>
        <Text small muted className="truncate" title={secondaryLabel}>
          {secondaryLabel}
        </Text>
      </td>

      {/* Source */}
      <td className="px-3 py-3">
        <ClientSourceBadge client={client} />
      </td>

      {/* Registered */}
      <td className="px-3 py-3">
        <Text small muted>
          {format(new Date(client.clientIdIssuedAt), "PP")}
        </Text>
      </td>

      {/* Actions */}
      <td className="px-3 py-3">
        <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
          <MoreActions actions={actions} />
        </div>
      </td>
    </DotRow>
  );

  return (
    <>
      {canRevoke ? (
        <ContextMenu>
          <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem onSelect={() => onViewSessions(client)}>
              View sessions
            </ContextMenuItem>
            <ContextMenuItem
              variant="destructive"
              onSelect={() => setConfirmOpen(true)}
            >
              Revoke client
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : (
        row
      )}

      <RevokeClientDialog
        client={client}
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        onRevoked={onRevoked}
      />
    </>
  );
}
