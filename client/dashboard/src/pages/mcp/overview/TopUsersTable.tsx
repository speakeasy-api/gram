import { IdentityLink } from "@/components/identity-link";
import { identityRefForKind } from "@/lib/identity-urn";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { Heading } from "@/components/ui/Heading";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { telemetryGetToolUsageUsers } from "@gram/client/funcs/telemetryGetToolUsageUsers";
import type { GetToolUsageUsersResult } from "@gram/client/models/components/gettoolusageusersresult.js";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { Column, Table } from "@/components/ui/Table";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

type UserRow = GetToolUsageUsersResult["users"][number];

const columns: Column<UserRow>[] = [
  {
    key: "userLabel",
    header: "User",
    render: (row) => (
      <Text className="truncate">
        <IdentityLink
          identifier={identityRefForKind(row.userKind, row.userKey)}
        >
          {row.userLabel}
        </IdentityLink>
      </Text>
    ),
  },
  {
    key: "eventCount",
    header: "Calls",
    width: "100px",
    render: (row) => <Text>{row.eventCount}</Text>,
  },
  {
    key: "failureCount",
    header: "Failures",
    width: "100px",
    render: (row) => <Text>{row.failureCount}</Text>,
  },
  {
    key: "uniqueTools",
    header: "Unique tools",
    width: "120px",
    render: (row) => <Text>{row.uniqueTools}</Text>,
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
          telemetryGetToolUsageUsers(client, {
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
      <Text muted small>
        Observability is not enabled for this organization.
      </Text>
    );
  } else if (isLoading) {
    content = <SkeletonTable />;
  } else if (error) {
    // A real failure (not the expected "logs disabled" 404) — surface it
    // instead of rendering the empty-results state, which would otherwise
    // read as "no usage" rather than "couldn't load usage".
    content = (
      <Text muted small className="text-destructive">
        Failed to load top users.
      </Text>
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
