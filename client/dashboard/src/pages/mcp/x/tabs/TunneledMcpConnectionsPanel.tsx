import { TunnelObservability } from "./TunnelObservability";
import { useTunneledMcpConnections } from "./tunnelStatus";

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
  const { data, isLoading, isError } = useTunneledMcpConnections(
    tunneledMcpServerId,
    { poll: true },
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
