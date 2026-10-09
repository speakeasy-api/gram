import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useEffect, useState } from "react";

export type SharedTunnelImpactState = {
  /** MCP servers on the tunnel that the caller can view. */
  servers: McpServer[];
  /**
   * The list was read successfully since the control became active, so a
   * confirmation can rely on it. A cached list from an earlier visit never
   * counts.
   */
  isReady: boolean;
  /** A read is still in flight: neither a fresh list nor a settled failure. */
  isLoading: boolean;
  isError: boolean;
  retry: () => void;
};

// The MCP servers that share one tunnel. Tunnel-wide controls read this when
// they open (`active`), and a confirmation stays disabled until that read
// lands, so what the user confirms is never a stale or failed list.
export function useSharedTunnelImpact(
  tunneledMcpServerId: string,
  { active }: { active: boolean },
): SharedTunnelImpactState {
  const [activatedAt, setActivatedAt] = useState(0);
  const query = useMcpServers({ tunneledMcpServerId }, undefined, {
    // Enabled only after the activation refetch below has started, so opening
    // a control sends one request rather than a mount fetch plus a refetch.
    enabled: active && activatedAt > 0 && tunneledMcpServerId !== "",
    staleTime: 0,
    // Shown inline with a retry, not thrown to the route's error boundary.
    throwOnError: false,
  });
  const { refetch } = query;

  useEffect(() => {
    if (!active) {
      setActivatedAt(0);
      return;
    }
    setActivatedAt(Date.now());
    void refetch();
  }, [active, tunneledMcpServerId, refetch]);

  const isReady =
    active &&
    activatedAt > 0 &&
    query.isSuccess &&
    !query.isFetching &&
    query.dataUpdatedAt >= activatedAt;

  // A retry after a failure keeps the query in its error status while it
  // fetches; that counts as loading, not as a failure.
  const isError = query.isError && !query.isFetching;

  return {
    // The list is filtered server-side; filtering again guards against a
    // stale or unfiltered cache hit listing servers on another tunnel.
    servers: (query.data?.mcpServers ?? []).filter(
      (server) => server.tunneledMcpServerId === tunneledMcpServerId,
    ),
    isReady,
    isLoading: active && !isReady && !isError,
    isError,
    retry: () => void refetch(),
  };
}

export const PUBLIC_SIBLING_WARNING =
  "Public MCP servers on this tunnel serve anonymous callers and bypass per-tool access control. Every MCP server on the tunnel reaches the same upstream service through the same tunnel agent and the credentials it holds, so the role grants on a private server do not protect a public one.";
