import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { Heading } from "@/components/ui/heading";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { telemetryGetToolUsageSummary } from "@gram/client/funcs/telemetryGetToolUsageSummary";
import type { GetToolUsageSummaryResult } from "@gram/client/models/components/gettoolusagesummaryresult.js";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { Table, type Column } from "@/components/ui/table";
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

  const { data, isLoading, error, isLogsDisabled } = useLogsEnabledErrorCheck(
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

  let content: React.JSX.Element;
  if (isLogsDisabled) {
    content = (
      <Type muted small>
        Observability is not enabled for this organization.
      </Type>
    );
  } else if (isLoading) {
    content = <SkeletonTable />;
  } else if (error) {
    // A real failure (not the expected "logs disabled" 404) — surface it
    // instead of rendering the empty-results state, which would otherwise
    // read as "no usage" rather than "couldn't load usage".
    content = (
      <Type muted small className="text-destructive">
        Failed to load top users.
      </Type>
    );
  } else {
    content = (
      <Table
        columns={columns}
        data={users}
        rowKey={(row) => row.userKey}
        noResultsMessage={<WidgetEmptyState message="No usage yet." />}
      />
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <Heading variant="h5">Top users</Heading>
      {content}
    </div>
  );
}
