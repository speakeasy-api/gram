import { useState } from "react";

import { ConnectionsList } from "@/components/connections/ConnectionsList";
import {
  CONNECTION_GROUPING_LABELS,
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

const GROUPING_OPTIONS: { value: ConnectionGrouping; label: string }[] = [
  { value: "subject", label: CONNECTION_GROUPING_LABELS.subject },
  { value: "issuer", label: CONNECTION_GROUPING_LABELS.issuer },
  { value: "provider", label: CONNECTION_GROUPING_LABELS.provider },
  { value: "client", label: CONNECTION_GROUPING_LABELS.client },
];

/**
 * Groupings worth offering for the scope a caller is showing.
 *
 * A grouping the caller has already fixed sorts a list of one: the MCP server
 * detail lists that server's own sessions, so filing them by MCP server draws
 * a single group and an option that appears to do nothing.
 */
function groupingOptions(hide: ConnectionGrouping[]) {
  return GROUPING_OPTIONS.filter((option) => !hide.includes(option.value));
}

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
  showGroupingControl = true,
  hideGroupings,
  bordered = true,
  emptyHeading = "No connections yet",
  emptyDescription = "Connections agents establish will appear here.",
  clients,
}: {
  sessions: UserSession[];
  /** Registrations for this scope; surfaced by the client grouping. */
  clients?: UserSessionClient[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  onRevoked: () => void;
  defaultGrouping?: ConnectionGrouping;
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
}): JSX.Element {
  const [grouping, setGrouping] = useState<ConnectionGrouping>(defaultGrouping);
  const project = useProject();
  const { hasScope } = useRBAC();
  // Scoped to the project these connections belong to, not the bare scope: a
  // user with project:write somewhere in the org must not be offered revoke
  // affordances here, where they would only fail at mutation time. Every
  // surface embedding this section is scoped to the working project.
  const canRevoke = hasScope("project:write", project.id);

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
      <Stack
        direction="horizontal"
        align="center"
        justify="space-between"
        gap={3}
      >
        <p className="text-destructive text-sm">
          Couldn&apos;t load connections.
        </p>
        <Button variant="tertiary" size="sm" onClick={onRetry}>
          Retry
        </Button>
      </Stack>
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
        onRevoked={onRevoked}
        clients={clients}
      />
    </Stack>
  );
}
