import { useState } from "react";

import {
  ConnectionsList,
  type ConnectionKillswitchContext,
} from "@/components/connections/ConnectionsList";
import {
  groupingOptions,
  type ConnectionGrouping,
} from "@/components/connections/groupConnections";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";

import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";

/**
 * Connections plus their grouping control and the loading / error / empty
 * branches, for surfaces that embed the list inside a larger page rather than
 * owning a toolbar of their own (the MCP server detail tab, the employee page).
 *
 * The organization page composes `ConnectionsList` directly instead, because
 * its grouping control belongs in `Page.Toolbar` alongside search and filters.
 */
export function ConnectionsListSection({
  sessions,
  isPending,
  isError,
  onRetry,
  onRevoked,
  defaultGrouping = "subject",
  grouping: controlledGrouping,
  showGroupingControl = true,
  hideGroupings,
  bordered = true,
  emptyHeading = "No connections yet",
  emptyDescription = "Connections agents establish will appear here.",
  clients,
  killswitchContext,
  revocable = true,
  canViewRegistrations,
}: {
  sessions: UserSession[];
  /** Registrations for this scope; surfaced by the client grouping. */
  clients?: UserSessionClient[];
  killswitchContext?: ConnectionKillswitchContext;
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  onRevoked: () => void;
  defaultGrouping?: ConnectionGrouping;
  /**
   * The grouping chosen by a control the caller renders itself, such as one in
   * its `Page.Toolbar`. Pair it with `showGroupingControl={false}`.
   */
  grouping?: ConnectionGrouping;
  /**
   * Whether the reader can regroup the list.
   *
   * Off for a surface whose scope has already answered the question the
   * control asks — one person's page groups by provider, and offering to
   * group that by person would sort a list of one.
   */
  showGroupingControl?: boolean;
  /**
   * Groupings to leave out because this surface has already fixed them — see
   * `groupingOptions`.
   */
  hideGroupings?: ConnectionGrouping[];
  /** Passed through to the list; see `ConnectionsList`. */
  bordered?: boolean;
  emptyHeading?: string;
  emptyDescription?: string;
  /**
   * Whether revoke actions may be offered at all. Off for a listing that spans
   * projects, where the working project is not the one a session belongs to.
   */
  revocable?: boolean;
  /** Passed through to the list; see `ConnectionsList`. */
  canViewRegistrations?: boolean;
}): JSX.Element {
  const [ownGrouping, setGrouping] =
    useState<ConnectionGrouping>(defaultGrouping);
  const grouping = controlledGrouping ?? ownGrouping;
  const project = useProject();
  const { hasScope } = useRBAC();
  // Scoped to the project these connections belong to, not the bare scope: a
  // user with project:write somewhere in the org must not be offered revoke
  // affordances here, where they would only fail at mutation time. Every
  // surface embedding this section is scoped to the working project, except
  // one that spans projects, which opts out through `revocable`.
  const canRevoke = revocable && hasScope("project:write", project.id);

  if (isPending) {
    return (
      <Stack gap={2}>
        {Array.from({ length: 3 }).map((_, index) => (
          <Skeleton key={index} className="h-24 w-full" />
        ))}
      </Stack>
    );
  }

  if (isError) {
    return (
      <div role="alert">
        <InlineEmptyState
          icon="triangle-alert"
          heading="Couldn't load connections"
          description="Check your connection, then try again."
          action={
            <Button variant="secondary" size="sm" onClick={onRetry}>
              Retry
            </Button>
          }
        />
      </div>
    );
  }

  if (sessions.length === 0 && (clients ?? []).length === 0) {
    return (
      <InlineEmptyState
        icon="unplug"
        heading={emptyHeading}
        description={emptyDescription}
      />
    );
  }

  return (
    // Wider than the gaps inside the table: the control changes what the whole
    // list is, so it has to read as sitting above the table rather than as its
    // first row.
    <Stack gap={6}>
      {showGroupingControl && (
        <Stack direction="horizontal" justify="end">
          <SegmentedControl
            value={grouping}
            onChange={(value: string) =>
              setGrouping(value as ConnectionGrouping)
            }
            options={groupingOptions(hideGroupings ?? [])}
          />
        </Stack>
      )}
      <ConnectionsList
        sessions={sessions}
        bordered={bordered}
        grouping={grouping}
        canRevoke={canRevoke}
        canViewRegistrations={canViewRegistrations}
        onRevoked={onRevoked}
        clients={clients}
        killswitchContext={killswitchContext}
      />
    </Stack>
  );
}
