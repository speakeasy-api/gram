import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import {
  useRemoteMcpTools,
  type RemoteMcpTool,
} from "@/hooks/useRemoteMcpTools";
import { useRemoteMcpUserSessionToken } from "@/hooks/useRemoteMcpUserSessionToken";
import { firstPartyConnectUrl, mcpConnectionUrl } from "@/lib/utils";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMemo } from "react";

export interface RemoteMcpConnection {
  /**
   * The Gram-origin display MCP URL (`/mcp/<slug>`) handed to the elements
   * chat, which manages its own transport (mirrors the toolset path).
   */
  mcpUrl: string | undefined;
  /** User-session JWT for issuer-gated servers, forwarded as a Bearer token. */
  gatewayToken: string | undefined;
  /** True when the upstream needs a connect dance before it will list tools. */
  needsAuth: boolean;
  /** First-party connect entry point to open when `needsAuth`. */
  connectUrl: string | undefined;
  /** Re-attempt the tools listing (e.g. after returning from the connect tab). */
  refetch: () => void;
  /** The tools advertised by the upstream, keyed by name. */
  tools: Record<string, RemoteMcpTool> | undefined;
  isLoading: boolean;
  /**
   * True once we can safely open the chat: a non-gated server is always ready,
   * a gated server waits for its minted JWT so the first request isn't a 401.
   */
  connectionReady: boolean;
}

/**
 * Resolves everything needed to chat with a remote-MCP-backed server: its
 * proxied `/mcp/<slug>` URL, the issuer-gated JWT, the live tools listing, and
 * the needs-connect state. Mirrors the plumbing in RemoteMcpToolsSection so the
 * playground connects through the exact path a real MCP client would.
 */
export function useRemoteMcpConnection(
  mcpServerId: string | undefined,
  isIssuerGated: boolean,
): RemoteMcpConnection {
  const { data: endpointsData, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ mcpServerId: mcpServerId ?? "" }, undefined, {
      enabled: !!mcpServerId,
    });
  const endpoints = useMemo(
    () => endpointsData?.mcpEndpoints ?? [],
    [endpointsData],
  );

  const { mcpUrl, loading: isResolvingUrl } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );

  const { accessToken, isLoading: isTokenLoading } =
    useRemoteMcpUserSessionToken({ mcpServerId, isIssuerGated });

  const headers = useMemo(
    () =>
      accessToken ? { Authorization: `Bearer ${accessToken}` } : undefined,
    [accessToken],
  );

  // Gated servers must wait for the JWT before connecting, otherwise the
  // unauthenticated request 401s and caches a spurious `needsAuth`.
  const connectionReady = !!mcpServerId && (!isIssuerGated || !!accessToken);

  // The tools listing (and needs-auth detection) uses the AI SDK MCP client,
  // which must connect same-origin via the dev proxy to carry credentials.
  const connectUrl = useMemo(() => mcpConnectionUrl(mcpUrl), [mcpUrl]);

  const { tools, needsAuth, isLoading, refetch } = useRemoteMcpTools(
    connectUrl,
    { headers, enabled: connectionReady },
  );

  const authUrl = useMemo(() => firstPartyConnectUrl(mcpUrl), [mcpUrl]);

  return {
    mcpUrl,
    gatewayToken: accessToken,
    needsAuth,
    connectUrl: authUrl,
    refetch,
    tools,
    isLoading: isResolvingUrl || isTokenLoading || isLoading,
    connectionReady,
  };
}
