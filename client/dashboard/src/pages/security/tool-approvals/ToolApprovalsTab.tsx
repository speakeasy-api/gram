import { Alert, AlertDescription } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { RiskPolicyApproval } from "@gram/client/models/components/riskpolicyapproval.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRiskListApprovals } from "@gram/client/react-query/riskListApprovals.js";
import { formatDistanceToNow } from "date-fns";
import { useState } from "react";
import { APPROVAL_STATUS_BADGE, requesterLabel } from "./approval-labels";
import { ToolApprovalSheet } from "./ToolApprovalSheet";

type StatusFilter = "pending" | "all";

/** How often an open queue checks for new requests: a held agent is waiting
 *  on someone, so a new request should show without a reload. */
const QUEUE_REFRESH_MS = 15_000;

/**
 * The Tool approvals tab: tool calls held by approval policies, pending ones
 * first by default. A row opens the request with its arguments and the
 * agent's reason; approvers decide there, the same as on the Slack card.
 */
export function ToolApprovalsTab(): JSX.Element {
  const [filter, setFilter] = useState<StatusFilter>("pending");
  const [selected, setSelected] = useState<RiskPolicyApproval | null>(null);
  const approvalsQuery = useRiskListApprovals(
    filter === "pending" ? { status: "pending", limit: 100 } : { limit: 100 },
    undefined,
    { refetchInterval: QUEUE_REFRESH_MS },
  );
  const membersQuery = useMembers();
  const members = membersQuery.data?.members ?? [];
  const approvals = approvalsQuery.data?.approvals ?? [];

  const columns: Column<RiskPolicyApproval>[] = [
    {
      key: "requester",
      header: "Requested by",
      width: "1fr",
      render: (approval) => (
        <Text small className="truncate">
          {requesterLabel(approval.requesterUrn, members)}
        </Text>
      ),
    },
    {
      key: "tool",
      header: "Tool",
      width: "1fr",
      render: (approval) => (
        <Text small mono className="truncate font-mono">
          {approval.toolName}
        </Text>
      ),
    },
    {
      key: "reason",
      header: "Reason",
      width: "2fr",
      render: (approval) => (
        <Text small muted={!approval.reason} className="truncate">
          {approval.reason ?? "Not requested yet"}
        </Text>
      ),
    },
    {
      key: "policy",
      header: "Policy",
      width: "1fr",
      render: (approval) => (
        <Text small muted className="truncate">
          {approval.policyName}
        </Text>
      ),
    },
    {
      key: "status",
      header: "Status",
      width: "0.8fr",
      render: (approval) => {
        const status = APPROVAL_STATUS_BADGE[approval.status];
        return (
          <Badge variant={status.variant}>
            <Badge.Text>{status.label}</Badge.Text>
          </Badge>
        );
      },
    },
    {
      key: "held",
      header: "Held",
      width: "0.8fr",
      render: (approval) => (
        <Text small muted>
          {formatDistanceToNow(approval.createdAt, { addSuffix: true })}
        </Text>
      ),
    },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 pb-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="max-w-2xl">
          <Heading variant="h4" className="mb-1">
            Tool approvals
          </Heading>
          <Text muted small>
            Tool calls held by approval policies. Approve once lets one retry of
            the exact call through; the agent is told either way.
          </Text>
        </div>
        <SegmentedControl
          value={filter}
          onChange={setFilter}
          options={[
            { value: "pending", label: "Pending" },
            { value: "all", label: "All" },
          ]}
        />
      </div>
      {approvalsQuery.isError ? (
        <Alert variant="error">
          <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
            <span>The approval requests could not be loaded.</span>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void approvalsQuery.refetch()}
            >
              <Button.Text>Retry</Button.Text>
            </Button>
          </AlertDescription>
        </Alert>
      ) : approvalsQuery.isPending ? (
        <SkeletonTable />
      ) : (
        <Table
          columns={columns}
          data={approvals}
          rowKey={(approval) => approval.id}
          onRowClick={setSelected}
          noResultsMessage={
            filter === "pending"
              ? "No calls are waiting for approval."
              : "No tool calls have been held yet."
          }
        />
      )}
      <ToolApprovalSheet
        approval={selected}
        members={members}
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
      />
    </div>
  );
}
