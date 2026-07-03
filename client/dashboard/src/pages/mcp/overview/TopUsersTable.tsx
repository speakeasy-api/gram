import { Heading } from "@/components/ui/heading";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { telemetryGetToolUsageSummary } from "@gram/client/funcs/telemetryGetToolUsageSummary";
import type { GetToolUsageSummaryResult } from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { Column, Table } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

type UserRow = GetToolUsageSummaryResult["users"][number];

const columns: Column<UserRow>[] = [
  {
    key: "userLabel",
    header: "User",
    render: (row) => <Type className="truncate">{row.userLabel}</Type>,
  },
  {
    key: "eventCount",
    header: "Calls",
    width: "100px",
    render: (row) => <Type>{row.eventCount}</Type>,
  },
  {
    key: "failureCount",
    header: "Failures",
    width: "100px",
    render: (row) => <Type>{row.failureCount}</Type>,
  },
  {
    key: "uniqueTools",
    header: "Unique tools",
    width: "120px",
    render: (row) => <Type>{row.uniqueTools}</Type>,
  },
];

export function TopUsersTable({
  toolsetSlug,
  from,
  to,
}: {
  toolsetSlug: string;
  from: Date;
  to: Date;
}): React.JSX.Element {
  const client = useGramContext();

  const { data, isLoading, isLogsDisabled } = useLogsEnabledErrorCheck(
    useQuery({
      queryKey: [
        "mcp-detail-top-users",
        toolsetSlug,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: () =>
        unwrapAsync(
          telemetryGetToolUsageSummary(client, {
            getToolUsageSummaryPayload: {
              from,
              to,
              hostedToolsetSlugs: [toolsetSlug],
            },
          }),
        ),
      throwOnError: false,
    }),
  );

  const users = useMemo(
    () =>
      [...(data?.users ?? [])]
        .sort((a, b) => b.eventCount - a.eventCount)
        .slice(0, 10),
    [data],
  );

  return (
    <div className="flex flex-col gap-3">
      <Heading variant="h5">Top users</Heading>
      {isLogsDisabled ? (
        <Type muted small>
          Observability is not enabled for this organization.
        </Type>
      ) : isLoading ? (
        <SkeletonTable />
      ) : (
        <Table
          columns={columns}
          data={users}
          rowKey={(row) => row.userKey}
          noResultsMessage={
            <Type muted className="block px-4 py-6">
              No usage yet.
            </Type>
          }
        />
      )}
    </div>
  );
}
