import { useMemo, useState } from "react";
import { ChevronRight } from "lucide-react";

import {
  connectionGroupSummary,
  groupAttentionState,
  groupConnections,
  groupUpstreams,
  splitByActivity,
  type ConnectionGroup,
  type ConnectionGrouping,
} from "@/components/connections/groupConnections";
import { RevokeClientDialog } from "@/components/sessions/RevokeClientDialog";
import { RevokeSessionDialog } from "@/components/sessions/RevokeSessionDialog";
import { RevokeSessionsDialog } from "@/components/sessions/RevokeSessionsDialog";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { MoreActions } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  CONNECTION_STATE_PRESENTATION,
  connectionActivityLabel,
  connectionDeadlineLabel,
  connectionState,
} from "@/lib/connection-state";
import { getInitials } from "@/lib/initials";
import { providerLabel } from "@/lib/provider-label";
import { subjectLabel } from "@/lib/user-session-status";
import { cn } from "@/lib/utils";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";

import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";

/**
 * Shared by the header, every group row, and every sub-row, so the whole tree
 * reads as one table however deep a branch is opened.
 *
 * Every track is sized explicitly rather than by `auto`. The header and each row
 * are separate grid containers, so an `auto` track resolves against whatever
 * that one container holds — "Connections" in the header and "3 connections" in
 * a row measure differently, and the columns drift apart. Only one track flexes,
 * and it sits ahead of nothing that has to align.
 */
const CONNECTION_ROW_GRID =
  "grid min-w-0 flex-1 items-center gap-x-4 text-left grid-cols-[0.875rem_1.5rem_minmax(0,1fr)] sm:grid-cols-[0.875rem_1.5rem_minmax(0,1fr)_10.5rem_11.5rem_7rem]";

/** Padding and gutter the header shares with every row, for the same reason. */
const CONNECTION_ROW_FRAME = "flex items-center gap-4 pr-3 pl-2";

/**
 * Width of the trailing actions slot, reserved on the header and on every row
 * including those with no actions — the grid beside it is `flex-1`, so a row
 * that dropped the slot would hand the extra width to its flexible column and
 * push everything after it out of line.
 */
const CONNECTION_ACTIONS_SLOT = "flex w-6 shrink-0 justify-end";

const GROUPING_HEADERS: Record<ConnectionGrouping, string> = {
  subject: "Person",
  provider: "Provider",
  client: "Client",
};

/** What a sub-row stands for, given what its parent row stands for. */
const CHILD_OF: Record<ConnectionGrouping, "client" | "person"> = {
  subject: "client",
  provider: "person",
  client: "person",
};

/**
 * The providers this group's subject holds tokens for, named.
 *
 * Kept to a tooltip and a count rather than a column: on a roster the question
 * is who has access and whether it is healthy, and giving providers a column of
 * their own put them at nearly the weight of the person's name.
 */
function providerNames(group: ConnectionGroup): string[] {
  return groupUpstreams(group).map((upstream) =>
    providerLabel(upstream.issuerSlug),
  );
}

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

/**
 * The face of whatever the row names: a person always gets their avatar, a
 * client always gets its product mark. The icon is the fastest way to tell what
 * kind of node a row is, so it leads every row at either level of the tree.
 */
function PersonIcon({
  label,
  photoUrl,
}: {
  label: string;
  photoUrl?: string;
}): JSX.Element {
  return (
    <Avatar className="size-6 shrink-0">
      {photoUrl ? <AvatarImage src={photoUrl} alt={label} /> : null}
      <AvatarFallback className="text-[9px] font-semibold">
        {getInitials(label)}
      </AvatarFallback>
    </Avatar>
  );
}

function ClientIcon({ label }: { label: string }): JSX.Element {
  return (
    <HookSourceIcon
      source={label}
      className="text-muted-foreground size-5 shrink-0"
    />
  );
}

function GroupIcon({ group }: { group: ConnectionGroup }): JSX.Element {
  if (group.identity) {
    return (
      <PersonIcon label={group.label} photoUrl={group.identity.photoUrl} />
    );
  }
  if (group.client) return <ClientIcon label={group.label} />;

  // Provider groups have neither an identity nor a registration to show, but
  // the cell still has to be occupied or every column after it shifts left.
  return <span className="size-6 shrink-0" />;
}

/**
 * One connection, rendered on the same grid as the row it hangs off.
 *
 * The indent comes from leaving the chevron and icon cells empty and hanging a
 * hairline off the name column: the child's own icon then starts where its
 * parent's name does, and every column to the right stays in line. Indenting the
 * whole row instead — a padded container — would have shifted health, activity
 * and status out of their columns, which is the thing that makes a tree stop
 * reading as a table.
 */
function ConnectionSubRow({
  session,
  grouping,
  actions,
}: {
  session: UserSession;
  grouping: ConnectionGrouping;
  actions?: React.ReactNode;
}): JSX.Element {
  const presentation = CONNECTION_STATE_PRESENTATION[connectionState(session)];
  const childIsPerson = CHILD_OF[grouping] === "person";
  const label = childIsPerson
    ? subjectLabel(session)
    : (session.clientName ?? "Unknown client");

  // A person's providers are a property of the person, not of each client they
  // connect through, so they belong on the parent row under person grouping and
  // on the sub-rows — one per person — under client grouping.
  const providers =
    grouping === "client"
      ? [
          ...new Set(
            (session.upstreams ?? []).map((upstream) =>
              providerLabel(upstream.issuerSlug),
            ),
          ),
        ]
      : [];

  return (
    // Hover has to be darker than the recessed ground the sub-rows sit on, or
    // pointing at one lightens it back towards the parent's white.
    <div className={cn(CONNECTION_ROW_FRAME, "hover:bg-muted/70 py-2")}>
      <div className={CONNECTION_ROW_GRID}>
        <span />
        <span />

        <span className="border-border flex min-w-0 items-center gap-2 border-l pl-3">
          {childIsPerson ? (
            <PersonIcon
              label={label}
              photoUrl={session.subjectPhotoUrl ?? undefined}
            />
          ) : (
            <ClientIcon label={label} />
          )}
          <span className="text-foreground truncate text-sm">{label}</span>
        </span>

        <SimpleTooltip tooltip={connectionDeadlineLabel(session)}>
          <span className="text-muted-foreground hidden truncate text-xs sm:block">
            {connectionActivityLabel(session.lastUsedAt)}
          </span>
        </SimpleTooltip>

        <span className="text-muted-foreground hidden truncate text-xs sm:block">
          {providers.join(", ")}
        </span>

        <span
          className={cn(
            "hidden truncate text-xs sm:block",
            presentation.toneClass,
          )}
        >
          {presentation.label}
        </span>
      </div>

      <span className={CONNECTION_ACTIONS_SLOT}>{actions}</span>
    </div>
  );
}

/**
 * One collapsed line per group, expanding into its connections.
 *
 * The page is a graph read one hop at a time: a row names a node, and opening it
 * shows the nodes on the other side of its edges — a person's clients, a
 * client's people. Collapsed by default because an admin scans for whoever has a
 * problem and only then wants the detail; a card per person made twenty
 * employees an unscrollable page.
 */
function ConnectionGroupRow({
  group,
  grouping,
  canRevoke,
  onRevoked,
}: {
  group: ConnectionGroup;
  grouping: ConnectionGrouping;
  canRevoke: boolean;
  onRevoked: () => void;
}): JSX.Element {
  const [expanded, setExpanded] = useState(false);
  const [revokeAllOpen, setRevokeAllOpen] = useState(false);
  const [revokeClientOpen, setRevokeClientOpen] = useState(false);

  const providers = grouping === "provider" ? [] : providerNames(group);

  const actions = [
    ...(group.revocableIds.length > 0
      ? [
          {
            label: "Revoke all connections",
            destructive: true,
            onClick: () => setRevokeAllOpen(true),
          },
        ]
      : []),
    ...(group.client
      ? [
          {
            label: "Revoke registration",
            destructive: true,
            onClick: () => setRevokeClientOpen(true),
          },
        ]
      : []),
  ];

  const attention = groupAttentionState(group);
  const attentionTone = attention
    ? CONNECTION_STATE_PRESENTATION[attention]
    : null;

  return (
    <div
      className={cn(
        // A hairline accent, and only when something is wrong. A healthy roster
        // stays completely quiet, so the eye lands on the exceptions.
        //
        // Bound to the same variables the status text uses, so the accent and
        // the word it stands for are the same colour in both themes. There is
        // no `border-l-default-warning` utility — only the `text-` ones are
        // declared — so the previous class silently rendered no accent at all.
        "border-l-2",
        attention === "needs_reauth" &&
          "border-l-[var(--text-default-destructive)]",
        attention === "expiring" && "border-l-[var(--text-default-warning)]",
        !attention && "border-l-transparent",
      )}
    >
      <div className={cn(CONNECTION_ROW_FRAME, "hover:bg-muted/30 py-2.5")}>
        <button
          type="button"
          onClick={() => setExpanded((open) => !open)}
          className={CONNECTION_ROW_GRID}
          aria-expanded={expanded}
        >
          <ChevronRight
            className={cn(
              "text-muted-foreground size-3.5 shrink-0 transition-transform",
              expanded && "rotate-90",
            )}
          />

          <GroupIcon group={group} />

          <span className="text-foreground truncate text-sm font-medium">
            {group.label}
          </span>

          <span className="text-muted-foreground hidden truncate text-xs sm:block">
            {connectionActivityLabel(
              group.lastUsedAt === null ? null : new Date(group.lastUsedAt),
            )}
          </span>

          <span className="text-muted-foreground hidden truncate text-xs tabular-nums sm:block">
            {connectionGroupSummary(group)}
            {providers.length > 0 ? (
              <SimpleTooltip tooltip={providers.join(", ")}>
                <span className="text-muted-foreground/70">
                  {" "}
                  · {providers.length} provider
                  {providers.length === 1 ? "" : "s"}
                </span>
              </SimpleTooltip>
            ) : grouping !== "provider" ? (
              // Said rather than left blank: reaching only Gram-native tools is
              // a real state, and an empty slot beside every other row's
              // provider count reads as data we failed to load.
              <span className="text-muted-foreground/70">
                {" "}
                · Gram tools only
              </span>
            ) : null}
          </span>

          <span
            className={cn(
              "hidden truncate text-xs sm:block",
              attentionTone?.toneClass,
            )}
          >
            {attentionTone?.label ?? ""}
          </span>
        </button>

        <span className={CONNECTION_ACTIONS_SLOT}>
          {canRevoke && actions.length > 0 ? (
            <MoreActions actions={actions} />
          ) : null}
        </span>
      </div>

      {expanded ? (
        // Recessed and fenced off at the top: the sub-rows share the parent's
        // columns, so without a ground of their own the first one reads as a
        // second line of the row it belongs to.
        <div className="bg-muted/40 border-border divide-border/60 divide-y border-t">
          {group.sessions.length === 0 ? (
            <div className="py-2 pr-3 pl-2">
              <span className="text-muted-foreground border-border ml-[2.5rem] border-l pl-3 text-xs">
                Registered but holds no connections.
              </span>
            </div>
          ) : null}

          {group.sessions.map((session) => (
            <ConnectionSubRow
              key={session.id}
              session={session}
              grouping={grouping}
              actions={
                canRevoke ? (
                  <ConnectionRowActions
                    session={session}
                    onRevoked={onRevoked}
                  />
                ) : null
              }
            />
          ))}
        </div>
      ) : null}

      <RevokeSessionsDialog
        sessionIds={group.revocableIds}
        open={revokeAllOpen}
        onOpenChange={setRevokeAllOpen}
        onRevoked={onRevoked}
      />

      {group.client ? (
        <RevokeClientDialog
          client={group.client}
          open={revokeClientOpen}
          onOpenChange={setRevokeClientOpen}
          onRevoked={onRevoked}
        />
      ) : null}
    </div>
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
  clients,
}: {
  sessions: UserSession[];
  grouping: ConnectionGrouping;
  canRevoke: boolean;
  onRevoked: () => void;
  /**
   * Registrations for this scope. Used only by client grouping, to surface
   * registrations that hold no connections — invisible otherwise, since the
   * grouping is derived from sessions.
   */
  clients?: UserSessionClient[];
}): JSX.Element {
  const { active, inactive } = useMemo(
    () => splitByActivity(groupConnections(sessions, grouping, { clients })),
    [sessions, grouping, clients],
  );

  const rows = (groups: ConnectionGroup[]) => (
    <div className="divide-border divide-y">
      {groups.map((group) => (
        <ConnectionGroupRow
          key={group.key}
          group={group}
          grouping={grouping}
          canRevoke={canRevoke}
          onRevoked={onRevoked}
        />
      ))}
    </div>
  );

  return (
    <div className="space-y-6">
      {/* Header and rows share one box rather than the header floating above
          it: unenclosed, the column labels read as a caption hanging off the
          grouping control above them instead of as the top of this table. */}
      <div className="border-border border">
        {/* Fixed to the body row's height rather than padded to approximate it:
            a group row is `py-2.5` around a `size-6` icon, so 44px. The header
            has no icon, so equal padding would leave it visibly shorter. */}
        <div
          className={cn(CONNECTION_ROW_FRAME, "border-border h-11 border-b")}
        >
          <div className={cn(CONNECTION_ROW_GRID, "text-eyebrow")}>
            {/* The chevron and icon cells hold no label, but still have to be
                occupied or the header labels sit left of the columns they
                name. */}
            <span />
            <span />
            <span>{GROUPING_HEADERS[grouping]}</span>
            <span className="hidden sm:block">Last used</span>
            <span className="hidden sm:block">Connections</span>
            <span className="hidden sm:block">Status</span>
          </div>
          <span className={CONNECTION_ACTIONS_SLOT} />
        </div>

        {active.length > 0 ? (
          rows(active)
        ) : (
          <Text small muted className="block px-2 py-3">
            Nothing has connected in the last week.
          </Text>
        )}
      </div>

      {/* Separated rather than filtered out: these still hold credentials worth
          revoking, and burying them behind a toggle is how a dead grant sits
          unnoticed for a year. Dimmed so they read as a footnote to the roster
          above rather than as more of it. */}
      {inactive.length > 0 ? (
        <div className="space-y-2 opacity-70">
          <p className="text-eyebrow px-2">
            Inactive · unused for over a week, or no longer usable
          </p>
          {/* No column header of its own — the labels above still apply, and
              repeating them would make this read as a second table rather than
              the tail of the first. */}
          <div className="border-border border">{rows(inactive)}</div>
        </div>
      ) : null}
    </div>
  );
}
