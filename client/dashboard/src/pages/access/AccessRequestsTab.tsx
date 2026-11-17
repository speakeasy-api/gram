import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useScopeCatalogue } from "@/hooks/useScopeCatalogue";
import type { AccessRequest } from "@gram/client/models/components/accessrequest.js";
import { useAccessRequests } from "@gram/client/react-query/accessRequests.js";
import { formatDistanceToNow } from "date-fns";
import { useMemo } from "react";
import { scopeLabel } from "./scopeCatalogue";

/**
 * Members' pending requests for access they were denied. Each row opens the
 * grant dialog on that request, where it is approved for a fixed window,
 * answered with a role, or denied.
 */
export function AccessRequestsTab({
  onReview,
}: {
  onReview: (request: AccessRequest) => void;
}): JSX.Element {
  const { catalogue } = useScopeCatalogue();
  const query = useAccessRequests({ status: "pending" });
  const rows = query.data?.requests ?? [];

  const columns = useMemo<Column<AccessRequest>[]>(
    () => [
      {
        key: "requesterName",
        header: "Requester",
        width: "1fr",
        render: (row) => (
          <Text
            variant="body"
            className="truncate font-medium"
            title={row.requesterName}
          >
            {row.requesterName}
          </Text>
        ),
      },
      {
        key: "scope",
        header: "Permission",
        width: "1fr",
        render: (row) => (
          <Text variant="body" className="truncate" title={row.scope}>
            {scopeLabel(catalogue, row.scope)}
          </Text>
        ),
      },
      {
        key: "resourceName",
        header: "Resource",
        width: "1fr",
        render: (row) => (
          <Text muted small className="truncate" title={row.resourceName}>
            {row.resourceName || "Every resource"}
          </Text>
        ),
      },
      {
        key: "createdAt",
        header: "Asked",
        width: "140px",
        render: (row) => (
          <Text muted small title={row.createdAt.toISOString()}>
            {formatDistanceToNow(row.createdAt, { addSuffix: true })}
          </Text>
        ),
      },
      {
        key: "actions",
        header: "",
        width: "100px",
        render: (row) => (
          <Button size="sm" variant="secondary" onClick={() => onReview(row)}>
            Review
          </Button>
        ),
      },
    ],
    [catalogue, onReview],
  );

  if (query.isPending) {
    return <SkeletonTable />;
  }
  if (query.isError) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load access requests"
        action={
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void query.refetch()}
          >
            Retry
          </Button>
        }
      />
    );
  }
  if (rows.length === 0) {
    return (
      <InlineEmptyState
        icon="inbox"
        heading="No pending access requests"
        description="When a member asks for access they were denied, it shows up here and in your Slack DMs. Requests lapse after 24 hours."
      />
    );
  }
  return <Table columns={columns} data={rows} rowKey={(row) => row.id} />;
}
