import type { ConnectionStatus } from "@gram/client/models/components/tunneledmcpserver.js";
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";

// Agent sessions come from heartbeats; polling at the connections panel's
// cadence lets a reconnected agent clear the offline state without a reload.
const TUNNEL_STATUS_POLL_MS = 15_000;

/**
 * Whether a successfully read connection status says no agent is serving the
 * tunnel. Anything else — connected, not yet read, or a read that failed (a
 * caller without project-level mcp:read cannot read the source) — is not
 * evidence the tunnel is offline.
 */
function tunnelAgentOffline(status: ConnectionStatus | undefined): boolean {
  return status === "inactive" || status === "never_connected";
}

export interface TunnelAgentStatus {
  /** A successful status read says no agent is connected. */
  offline: boolean;
  refetch: () => void;
}

/**
 * Reads whether the tunnel behind a server has a connected agent. A failed
 * tools listing alone cannot tell: the gateway answers an offline tunnel with
 * the same not-found it uses for every unroutable request.
 */
export function useTunnelAgentStatus({
  tunneledSourceId,
  projectSlug,
  enabled,
  poll,
}: {
  tunneledSourceId: string | undefined;
  /** The source's project, for org-level pages with no ambient project. */
  projectSlug?: string;
  enabled: boolean;
  /** Keep re-reading, e.g. while the tools listing is failing. */
  poll: boolean;
}): TunnelAgentStatus {
  const query = useGetTunneledMcpServer(
    { id: tunneledSourceId ?? "", gramProject: projectSlug },
    undefined,
    {
      enabled: enabled && !!tunneledSourceId,
      throwOnError: false,
      retry: false,
      refetchInterval: poll ? TUNNEL_STATUS_POLL_MS : false,
      refetchIntervalInBackground: false,
    },
  );

  return {
    offline:
      query.isSuccess && tunnelAgentOffline(query.data?.connectionStatus),
    refetch: () => {
      void query.refetch();
    },
  };
}
