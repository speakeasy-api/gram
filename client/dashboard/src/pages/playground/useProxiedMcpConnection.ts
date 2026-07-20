import {
  useProxiedMcpTools,
  type ProxiedMcpTool,
} from "@/hooks/useProxiedMcpTools";
import { useProxiedMcpUserSessionToken } from "@/hooks/useProxiedMcpUserSessionToken";
import {
  firstPartyConnectUrl,
  getServerURL,
  mcpConnectionUrl,
} from "@/lib/utils";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMemo } from "react";

export interface ProxiedMcpConnection {
  /**
   * The Gram-origin display MCP URL (`/mcp/<slug>`) handed to the elements
   * chat, which manages its own transport (mirrors the toolset path).
   */
  mcpUrl: string | undefined;
  /** User-session JWT for issuer-gated servers, forwarded as a Bearer token. */
  gatewayToken: string | undefined;
  /** True when the upstream needs a connect dance before it will list tools. */
  needsAuth: boolean;
  /** True when listing tools failed for a non-auth reason (network, 5xx, …). */
  isError: boolean;
  /** First-party connect entry point to open when `needsAuth`. */
  connectUrl: string | undefined;
  /** Re-attempt the tools listing (e.g. after returning from the connect tab). */
  refetch: () => void;
  /** The tools advertised by the upstream, keyed by name. */
  tools: Record<string, ProxiedMcpTool> | undefined;
  isLoading: boolean;
  /**
   * True once we can safely open the chat: a non-gated server is always ready,
   * a gated server waits for its minted JWT so the first request isn't a 401.
   * Stays false if the mint fails, so callers block rather than opening an
   * unauthenticated chat.
   */
  connectionReady: boolean;
}

/**
 * Resolves everything needed to chat with a proxied-MCP-backed server: its
 * proxied `/mcp/<slug>` URL, the issuer-gated JWT, the live tools listing, and
 * the needs-connect state. Mirrors the plumbing in RemoteMcpToolsSection so the
 * playground connects through the exact path a real MCP client would.
 */
export function useProxiedMcpConnection(
  mcpServerId: string | undefined,
  isIssuerGated: boolean,
): ProxiedMcpConnection {
  const { data: endpointsData, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ mcpServerId: mcpServerId ?? "" }, undefined, {
      enabled: !!mcpServerId,
    });

  // Always address the platform `/mcp/<slug>` origin, never a custom domain:
  // the playground chat is configured against the Gram/proxy origin, so a
  // custom-domain URL would be a cross-origin mismatch (mirrors useInternalMcpUrl).
  const mcpUrl = useMemo(() => {
    const endpoints = endpointsData?.mcpEndpoints ?? [];
    // Only a platform-domain endpoint (no customDomainId) has a slug registered
    // on the Gram origin. A custom-domain endpoint's slug lives under that
    // domain, so `${getServerURL()}/mcp/<slug>` would 404 — never fall back to
    // one here. A server with only custom-domain endpoints has no Gram-origin
    // URL, so we return undefined (the chat surfaces the not-connected state).
    const endpoint = endpoints.find((e) => !e.customDomainId);
    return endpoint ? `${getServerURL()}/mcp/${endpoint.slug}` : undefined;
  }, [endpointsData]);

  const { accessToken, isLoading: isTokenLoading } =
    useProxiedMcpUserSessionToken({ mcpServerId, isIssuerGated });

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

  // No error boundary wraps the playground panels, so keep every failure inline
  // (isError) rather than throwing a non-401 to the nearest boundary.
  const { tools, needsAuth, isError, isLoading, refetch } = useProxiedMcpTools(
    connectUrl,
    { headers, enabled: connectionReady, throwOnError: false },
  );

  const authUrl = useMemo(() => firstPartyConnectUrl(mcpUrl), [mcpUrl]);

  return {
    mcpUrl,
    gatewayToken: accessToken,
    needsAuth,
    isError,
    connectUrl: authUrl,
    refetch,
    tools,
    isLoading: isLoadingEndpoints || isTokenLoading || isLoading,
    connectionReady,
  };
}
