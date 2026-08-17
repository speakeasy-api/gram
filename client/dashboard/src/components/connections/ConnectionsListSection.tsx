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
import { useRBAC } from "@/hooks/useRBAC";

import type { UserSession } from "@gram/client/models/components/usersession.js";

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
}: {
  sessions: UserSession[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  onRevoked: () => void;
  defaultGrouping?: ConnectionGrouping;
  emptyHeading?: string;
  emptyDescription?: string;
}): JSX.Element {
  const [grouping, setGrouping] = useState<ConnectionGrouping>(defaultGrouping);
  const { hasScope } = useRBAC();
  const canRevoke = hasScope("project:write");

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

  if (sessions.length === 0) {
    return (
      <InlineEmptyState
        icon="unplug"
        heading={emptyHeading}
        description={emptyDescription}
      />
    );
  }

  return (
    <Stack gap={3}>
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
      />
    </Stack>
  );
}
