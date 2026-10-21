import { TunnelObservability } from "./TunnelObservability";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import { useListTunneledMcpServerConnections } from "@gram/client/react-query/listTunneledMcpServerConnections.js";

// Live sessions come from Redis heartbeats. The status view polls on this
// cadence and shows its own error state when a poll fails.
const TUNNEL_STATUS_POLL_MS = 30_000;

// Shows tunnel status, connected agents and activity for one tunnel source.
export function TunneledMcpConnectionsPanel({
  tunneledMcpServerId,
  agentSetupHref,
  logsHref,
}: {
  tunneledMcpServerId: string;
  /** Settings anchor with the agent snippets, offered when nothing is connected. */
  agentSetupHref: string;
  logsHref: string;
}): JSX.Element {
  const { data, isLoading, isError } = useListTunneledMcpServerConnections(
    getTunneledMcpServerArgs(tunneledMcpServerId),
    undefined,
    {
      // The status view keeps rendering when a poll fails.
      throwOnError: false,
      refetchInterval: TUNNEL_STATUS_POLL_MS,
      refetchIntervalInBackground: false,
    },
  );

  return (
    <TunnelObservability
      id={tunneledMcpServerId}
      connections={data}
      loading={isLoading}
      error={isError}
      logsHref={logsHref}
      agentSetupHref={agentSetupHref}
    />
  );
}
