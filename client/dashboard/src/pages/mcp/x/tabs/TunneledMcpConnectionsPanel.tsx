import { InlineEmptyState } from "@/components/inline-empty-state";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { dateTimeFormatters } from "@/lib/dates";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import type { ConnectionStatus } from "@gram/client/models/components/tunneledmcpserver.js";
import type { TunneledMcpConnection } from "@gram/client/models/components/tunneledmcpconnection.js";
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { useListTunneledMcpServerConnections } from "@gram/client/react-query/listTunneledMcpServerConnections.js";
import { formatDistanceToNow } from "date-fns";
import { Link } from "react-router";

// Live sessions come from Redis heartbeats; a short poll keeps the panel
// honest without hammering the API while the page sits open.
const CONNECTIONS_POLL_MS = 15_000;

type StatusPresentation = {
  label: string;
  variant: "success" | "warning" | "neutral";
};

function connectionStatusPresentation(
  status: ConnectionStatus | undefined,
): StatusPresentation {
  switch (status) {
    case "connected":
      return { label: "Connected", variant: "success" };
    case "inactive":
      return { label: "Inactive", variant: "warning" };
    case "never_connected":
      return { label: "Never connected", variant: "neutral" };
    case undefined:
      return { label: "Unknown", variant: "neutral" };
  }
}

function relative(date: Date): string {
  return formatDistanceToNow(date, { addSuffix: true });
}

const columns: Column<TunneledMcpConnection>[] = [
  {
    key: "gatewaySessionId",
    header: "Session",
    render: (connection) => (
      <Text small mono className="truncate" title={connection.gatewaySessionId}>
        {connection.gatewaySessionId}
      </Text>
    ),
  },
  {
    key: "agentVersion",
    header: "Agent",
    width: "120px",
    render: (connection) => (
      <Text small mono>
        {connection.agentVersion ?? "-"}
      </Text>
    ),
  },
  {
    key: "serviceVersion",
    header: "Service",
    width: "120px",
    render: (connection) => (
      <Text small mono>
        {connection.serviceVersion}
      </Text>
    ),
  },
  {
    key: "connectedAt",
    header: "Connected",
    width: "160px",
    render: (connection) => (
      <Text small>
        {dateTimeFormatters.humanize(new Date(connection.connectedAt))}
      </Text>
    ),
  },
  {
    key: "lastHeartbeatAt",
    header: "Heartbeat",
    width: "160px",
    render: (connection) => (
      <Text small>{relative(new Date(connection.lastHeartbeatAt))}</Text>
    ),
  },
  {
    key: "activeConsumerSessions",
    header: "Sessions",
    width: "90px",
    render: (connection) => (
      <Text small>{connection.activeConsumerSessions}</Text>
    ),
  },
  {
    key: "activeSubstreams",
    header: "Streams",
    width: "90px",
    render: (connection) => <Text small>{connection.activeSubstreams}</Text>,
  },
  {
    key: "remoteAddr",
    header: "Remote address",
    width: "160px",
    render: (connection) => (
      <Text small mono>
        {connection.remoteAddr ?? "-"}
      </Text>
    ),
  },
];

// Ported from the retired tunneled source page's overview: which agents are
// holding the tunnel open right now, and when the source was last seen.
export function TunneledMcpConnectionsPanel({
  tunneledMcpServerId,
  agentSetupHref,
}: {
  tunneledMcpServerId: string;
  /** Settings anchor with the agent snippets, offered when nothing is connected. */
  agentSetupHref: string;
}): JSX.Element {
  // The status badge and last-seen come from the source row, so it polls on
  // the same cadence as the connections table or it would go stale as agents
  // come and go.
  const { data: source } = useGetTunneledMcpServer(
    getTunneledMcpServerArgs(tunneledMcpServerId),
    undefined,
    {
      refetchInterval: CONNECTIONS_POLL_MS,
      refetchIntervalInBackground: false,
    },
  );
  const { data, isLoading, isError, refetch } =
    useListTunneledMcpServerConnections(
      getTunneledMcpServerArgs(tunneledMcpServerId),
      undefined,
      {
        refetchInterval: CONNECTIONS_POLL_MS,
        refetchIntervalInBackground: false,
      },
    );

  const connections = data?.connections ?? [];
  const status = connectionStatusPresentation(source?.connectionStatus);
  const lastSeen = source?.lastSeenAt
    ? relative(new Date(source.lastSeenAt))
    : "never";

  let body = (
    <Table
      columns={columns}
      data={connections}
      rowKey={(connection) => connection.gatewaySessionId}
    />
  );
  if (isLoading) {
    body = <SkeletonTable />;
  } else if (isError && !data) {
    // A failed poll with nothing loaded yet is not "no connections".
    body = (
      <div className="flex flex-col items-start gap-2 py-6">
        <Text muted>Failed to load tunnel connections.</Text>
        <Button size="sm" variant="secondary" onClick={() => void refetch()}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </div>
    );
  } else if (connections.length === 0) {
    body = (
      <InlineEmptyState
        icon="plug"
        heading="No live tunnel connections"
        description="Start a tunnel agent next to the upstream MCP server to connect this source."
        action={
          <Link to={agentSetupHref}>
            <Button variant="secondary" size="sm">
              <Button.Text>View agent setup</Button.Text>
            </Button>
          </Link>
        }
      />
    );
  }

  return (
    <div className="border p-5">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-eyebrow">Tunnel connections</h3>
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={status.variant}>
            <Badge.Text>{status.label}</Badge.Text>
          </Badge>
          <Badge variant="neutral">
            <Badge.Text>{data?.activeConnectionCount ?? 0} active</Badge.Text>
          </Badge>
          <Badge variant="neutral">
            <Badge.Text>
              {data?.activeConsumerSessionCount ?? 0} consumer sessions
            </Badge.Text>
          </Badge>
        </div>
      </div>
      <div className="mb-4 flex flex-wrap gap-x-6 gap-y-1">
        <Text small muted>
          Last seen {lastSeen}
        </Text>
        {source?.keyPrefix && (
          <Text small muted>
            Key prefix <span className="font-mono">{source.keyPrefix}</span>
          </Text>
        )}
      </div>
      {body}
    </div>
  );
}
