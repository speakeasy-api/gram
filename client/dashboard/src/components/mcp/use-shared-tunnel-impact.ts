import { serversMatchingFilter } from "@/pages/mcp/x/tabs/settings/sections/sourceDelete";
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
  const filter = { tunneledMcpServerId };
  const query = useMcpServers(filter, undefined, {
    enabled: active && tunneledMcpServerId !== "",
    staleTime: 0,
    // Shown inline with a retry, not thrown to the route's error boundary.
    throwOnError: false,
  });
  const [activatedAt, setActivatedAt] = useState(0);
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

  return {
    servers: serversMatchingFilter(filter, query.data?.mcpServers ?? []),
    isReady,
    isLoading: active && !isReady && !query.isError,
    isError: query.isError && !query.isFetching,
    retry: () => void refetch(),
  };
}

export const PUBLIC_SIBLING_WARNING =
  "Public MCP servers on this tunnel serve anonymous callers and bypass per-tool access control. Every MCP server on the tunnel reaches the same upstream service through the same tunnel agent and the credentials it holds, so the role grants on a private server do not protect a public one.";
