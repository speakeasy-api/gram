import { useState } from "react";

import {
  ConnectionsList,
  type ConnectionKillswitchContext,
} from "@/components/connections/ConnectionsList";
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
  { value: "provider", label: CONNECTION_GROUPING_LABELS.provider },
  { value: "client", label: CONNECTION_GROUPING_LABELS.client },
];

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
  emptyHeading = "No connections yet",
  emptyDescription = "Connections agents establish will appear here.",
  clients,
  killswitchContext,
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
      <Stack direction="horizontal" justify="end">
        <SegmentedControl
          value={grouping}
          onChange={(value: string) => setGrouping(value as ConnectionGrouping)}
          options={GROUPING_OPTIONS}
        />
      </Stack>
      <ConnectionsList
        sessions={sessions}
        grouping={grouping}
        canRevoke={canRevoke}
        onRevoked={onRevoked}
        clients={clients}
        killswitchContext={killswitchContext}
      />
    </Stack>
  );
}
