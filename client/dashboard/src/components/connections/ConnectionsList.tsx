import { useMemo, useState } from "react";

import { ConnectionChainRow } from "@/components/connections/ConnectionChainRow";
import {
  connectionGroupSummary,
  groupConnections,
  type ConnectionGroup,
  type ConnectionGrouping,
} from "@/components/connections/groupConnections";
import { RevokeSessionDialog } from "@/components/sessions/RevokeSessionDialog";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { RevokeSessionsDialog } from "@/components/sessions/RevokeSessionsDialog";
import { Button } from "@/components/ui/Button";
import { MoreActions } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { getInitials } from "@/lib/initials";

import type { UserSession } from "@gram/client/models/components/usersession.js";

function ConnectionRowActions({
  session,
  onRevoked,
}: {
  session: UserSession;
  onRevoked: () => void;
}): JSX.Element {
  const [confirmOpen, setConfirmOpen] = useState(false);

  return (
    <>
      <MoreActions
        actions={[
          {
            label: "Revoke connection",
            destructive: true,
            onClick: () => setConfirmOpen(true),
          },
        ]}
      />
      <RevokeSessionDialog
        session={session}
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        onRevoked={onRevoked}
      />
    </>
  );
}

function ConnectionGroupSection({
  group,
  canRevoke,
  onRevoked,
  showSubject,
}: {
  group: ConnectionGroup;
  canRevoke: boolean;
  onRevoked: () => void;
  /** True when the heading is not a person, so the rows must name one. */
  showSubject: boolean;
}): JSX.Element {
  const [revokeAllOpen, setRevokeAllOpen] = useState(false);

  return (
    <section className="border-border border">
      <header className="border-border bg-card flex items-center justify-between gap-3 border-b px-4 py-3">
        <div className="flex min-w-0 items-center gap-3">
          {group.identity ? (
            <Avatar className="size-8 shrink-0">
              {group.identity.photoUrl ? (
                <AvatarImage src={group.identity.photoUrl} alt={group.label} />
              ) : null}
              <AvatarFallback className="text-[10px] font-semibold">
                {getInitials(group.label)}
              </AvatarFallback>
            </Avatar>
          ) : null}
          <div className="min-w-0">
            <Text className="truncate font-medium">{group.label}</Text>
            <Text small muted>
              {connectionGroupSummary(group)}
            </Text>
          </div>
        </div>
        {canRevoke && group.revocableIds.length > 0 ? (
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setRevokeAllOpen(true)}
          >
            Revoke all
          </Button>
        ) : null}
      </header>

      <div className="divide-border divide-y">
        {group.sessions.map((session) => (
          <ConnectionChainRow
            key={`${group.key}:${session.id}`}
            session={session}
            showSubject={showSubject}
            actions={
              canRevoke ? (
                <ConnectionRowActions session={session} onRevoked={onRevoked} />
              ) : undefined
            }
          />
        ))}
      </div>

      <RevokeSessionsDialog
        sessionIds={group.revocableIds}
        open={revokeAllOpen}
        onOpenChange={setRevokeAllOpen}
        onRevoked={onRevoked}
      />
    </section>
  );
}

/**
 * The connection surface, rendered identically wherever connections are shown —
 * organization-wide, scoped to one MCP server, or scoped to one person. The
 * scoping is the caller's job: this component only ever renders the sessions it
 * is handed, so the three surfaces cannot drift apart in how a connection reads.
 */
export function ConnectionsList({
  sessions,
  grouping,
  canRevoke,
  onRevoked,
}: {
  sessions: UserSession[];
  grouping: ConnectionGrouping;
  canRevoke: boolean;
  onRevoked: () => void;
}): JSX.Element {
  const groups = useMemo(
    () => groupConnections(sessions, grouping),
    [sessions, grouping],
  );

  return (
    <div className="space-y-4">
      {groups.map((group) => (
        <ConnectionGroupSection
          key={group.key}
          group={group}
          canRevoke={canRevoke}
          onRevoked={onRevoked}
          showSubject={grouping !== "subject"}
        />
      ))}
    </div>
  );
}
