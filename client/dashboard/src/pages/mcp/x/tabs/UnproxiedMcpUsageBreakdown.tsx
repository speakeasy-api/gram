import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { type Column, Table } from "@/components/ui/Table";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useGetUnproxiedMcpServerToolUsage } from "@gram/client/react-query/getUnproxiedMcpServerToolUsage.js";
import { useGetUnproxiedMcpServerUserUsage } from "@gram/client/react-query/getUnproxiedMcpServerUserUsage.js";
import { useGetUnproxiedMcpServerClientUsage } from "@gram/client/react-query/getUnproxiedMcpServerClientUsage.js";
import type { UnproxiedMcpServerToolUsageRow } from "@gram/client/models/components/unproxiedmcpservertoolusagerow.js";
import type { UnproxiedMcpServerUserUsageRow } from "@gram/client/models/components/unproxiedmcpserveruserusagerow.js";
import type { UnproxiedMcpServerClientUsageRow } from "@gram/client/models/components/unproxiedmcpserverclientusagerow.js";
import { useEffect, useState } from "react";

const PAGE_LIMIT = 25;

const toolColumns: Column<UnproxiedMcpServerToolUsageRow>[] = [
  {
    key: "toolName",
    header: "Tool",
    render: (row) => <Text className="truncate">{row.toolName}</Text>,
  },
  {
    key: "callCount",
    header: "Calls",
    width: "100px",
    render: (row) => <Text>{row.callCount}</Text>,
  },
  {
    key: "failureCount",
    header: "Failures",
    width: "100px",
    render: (row) => <Text>{row.failureCount}</Text>,
  },
];

const userColumns: Column<UnproxiedMcpServerUserUsageRow>[] = [
  {
    key: "userEmail",
    header: "User",
    render: (row) => (
      <Text className="truncate">{row.userEmail || "Unknown"}</Text>
    ),
  },
  {
    key: "callCount",
    header: "Calls",
    width: "100px",
    render: (row) => <Text>{row.callCount}</Text>,
  },
  {
    key: "lastCalledAt",
    header: "Last called",
    width: "180px",
    render: (row) => <Text>{row.lastCalledAt.toLocaleDateString()}</Text>,
  },
];

const clientColumns: Column<UnproxiedMcpServerClientUsageRow>[] = [
  {
    key: "client",
    header: "Client",
    render: (row) => <Text className="truncate">{row.client}</Text>,
  },
  {
    key: "callCount",
    header: "Calls",
    width: "100px",
    render: (row) => <Text>{row.callCount}</Text>,
  },
];

// Presentational only — each *UsageTable component below owns its own
// cursor/rows state (they can't share a hook cleanly: the query needs the
// cursor as an input, but the accumulated rows depend on the query's output,
// so the state has to live beside its query, not behind a shared hook that
// would need calling twice — once for the cursor, once for the result —
// which would just create two disconnected state instances).
function UsageTable<Row extends object>({
  columns,
  rows,
  hasMore,
  onLoadMore,
  isInitialLoading,
  isLoadingMore,
  isError,
  emptyMessage,
  rowKey,
}: {
  columns: Column<Row>[];
  rows: Row[];
  hasMore: boolean;
  onLoadMore: () => void;
  isInitialLoading: boolean;
  isLoadingMore: boolean;
  isError: boolean;
  emptyMessage: string;
  rowKey: (row: Row) => string | number;
}): JSX.Element {
  if (isInitialLoading) {
    return <SkeletonTable />;
  }
  if (isError) {
    return (
      <Text muted small className="text-destructive">
        Couldn't load this table. Try refreshing the page.
      </Text>
    );
  }

  return (
    <Table columns={columns}>
      <Table.Header columns={columns} />
      <Table.Body
        columns={columns}
        data={rows}
        rowKey={rowKey}
        hasMore={hasMore}
        handleLoadMore={onLoadMore}
        isLoading={isLoadingMore}
        noResultsMessage={
          <Text muted small>
            {emptyMessage}
          </Text>
        }
      />
    </Table>
  );
}

function ToolsUsageTable({
  url,
  from,
  to,
}: {
  url: string;
  from: Date;
  to: Date;
}): JSX.Element {
  const [scope, setScope] = useState(url);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [rows, setRows] = useState<UnproxiedMcpServerToolUsageRow[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);

  if (scope !== url) {
    setScope(url);
    setCursor(undefined);
    setRows([]);
    setNextCursor(undefined);
  }

  const query = useGetUnproxiedMcpServerToolUsage(
    {
      unproxiedMcpServerUsageBreakdownPayload: {
        url,
        from,
        to,
        limit: PAGE_LIMIT,
        cursor,
      },
    },
    undefined,
    { enabled: !!url, throwOnError: false },
  );

  useEffect(() => {
    if (!query.data) return;
    setRows((prev) =>
      cursor ? [...prev, ...query.data.tools] : query.data.tools,
    );
    setNextCursor(query.data.nextCursor);
    // Only a newly-arrived page should append/replace rows; cursor is read
    // via closure (whatever was in flight when this page resolved) rather
    // than a reactive dependency, since including it would refire this
    // effect as soon as loadMore sets a new cursor, before the new page has
    // actually come back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.data]);

  return (
    <UsageTable
      columns={toolColumns}
      rows={rows}
      hasMore={!!nextCursor}
      onLoadMore={() => {
        if (nextCursor) setCursor(nextCursor);
      }}
      isInitialLoading={query.isLoading && rows.length === 0}
      isLoadingMore={query.isFetching && rows.length > 0}
      isError={query.isError}
      emptyMessage="No tool calls observed yet."
      rowKey={(row) => row.toolName}
    />
  );
}

function UsersUsageTable({
  url,
  from,
  to,
}: {
  url: string;
  from: Date;
  to: Date;
}): JSX.Element {
  const [scope, setScope] = useState(url);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [rows, setRows] = useState<UnproxiedMcpServerUserUsageRow[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);

  if (scope !== url) {
    setScope(url);
    setCursor(undefined);
    setRows([]);
    setNextCursor(undefined);
  }

  const query = useGetUnproxiedMcpServerUserUsage(
    {
      unproxiedMcpServerUsageBreakdownPayload: {
        url,
        from,
        to,
        limit: PAGE_LIMIT,
        cursor,
      },
    },
    undefined,
    { enabled: !!url, throwOnError: false },
  );

  useEffect(() => {
    if (!query.data) return;
    setRows((prev) =>
      cursor ? [...prev, ...query.data.users] : query.data.users,
    );
    setNextCursor(query.data.nextCursor);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.data]);

  return (
    <UsageTable
      columns={userColumns}
      rows={rows}
      hasMore={!!nextCursor}
      onLoadMore={() => {
        if (nextCursor) setCursor(nextCursor);
      }}
      isInitialLoading={query.isLoading && rows.length === 0}
      isLoadingMore={query.isFetching && rows.length > 0}
      isError={query.isError}
      emptyMessage="No user activity observed yet."
      rowKey={(row) => row.userEmail || "unknown"}
    />
  );
}

function ClientsUsageTable({
  url,
  from,
  to,
}: {
  url: string;
  from: Date;
  to: Date;
}): JSX.Element {
  const [scope, setScope] = useState(url);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [rows, setRows] = useState<UnproxiedMcpServerClientUsageRow[]>([]);
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined);

  if (scope !== url) {
    setScope(url);
    setCursor(undefined);
    setRows([]);
    setNextCursor(undefined);
  }

  const query = useGetUnproxiedMcpServerClientUsage(
    {
      unproxiedMcpServerUsageBreakdownPayload: {
        url,
        from,
        to,
        limit: PAGE_LIMIT,
        cursor,
      },
    },
    undefined,
    { enabled: !!url, throwOnError: false },
  );

  useEffect(() => {
    if (!query.data) return;
    setRows((prev) =>
      cursor ? [...prev, ...query.data.clients] : query.data.clients,
    );
    setNextCursor(query.data.nextCursor);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.data]);

  return (
    <UsageTable
      columns={clientColumns}
      rows={rows}
      hasMore={!!nextCursor}
      onLoadMore={() => {
        if (nextCursor) setCursor(nextCursor);
      }}
      isInitialLoading={query.isLoading && rows.length === 0}
      isLoadingMore={query.isFetching && rows.length > 0}
      isError={query.isError}
      emptyMessage="No client activity observed yet."
      rowKey={(row) => row.client}
    />
  );
}

/**
 * Tabbed tool/user/client usage breakdown for an unproxied MCP server, sourced
 * from the same Shadow-MCP-matched-by-URL data as the overview chart above it
 * (same best-effort coverage caveats apply).
 */
export function UnproxiedMcpUsageBreakdown({
  url,
  from,
  to,
}: {
  url: string;
  from: Date;
  to: Date;
}): JSX.Element {
  return (
    <Tabs defaultValue="tools">
      <TabsList>
        <TabsTrigger value="tools">Tools</TabsTrigger>
        <TabsTrigger value="users">Users</TabsTrigger>
        <TabsTrigger value="clients">Clients</TabsTrigger>
      </TabsList>
      <TabsContent value="tools">
        <ToolsUsageTable url={url} from={from} to={to} />
      </TabsContent>
      <TabsContent value="users">
        <UsersUsageTable url={url} from={from} to={to} />
      </TabsContent>
      <TabsContent value="clients">
        <ClientsUsageTable url={url} from={from} to={to} />
      </TabsContent>
    </Tabs>
  );
}
