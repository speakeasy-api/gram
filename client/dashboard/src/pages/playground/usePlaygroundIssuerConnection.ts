import { useRemoteMcpTools } from "@/hooks/useRemoteMcpTools";
import { useToolsetUserSessionToken } from "@/hooks/useToolsetUserSessionToken";
import { useInternalMcpUrl } from "@/hooks/useToolsetUrl";
import { Toolset } from "@/lib/toolTypes";
import { firstPartyConnectUrl, mcpConnectionUrl } from "@/lib/utils";
import { useCallback, useMemo } from "react";

export interface PlaygroundIssuerConnection {
  /** True when the toolset is issuer-gated (has a user_session_issuer). */
  isIssuerGated: boolean;
  /** The minted user-session JWT for the chat `Authorization: Bearer` header. */
  accessToken: string | undefined;
  /** The upstream session is linked — the probe listed tools successfully. */
  connected: boolean;
  /** The probe 401'd: the user must run the first-party connect flow. */
  needsAuth: boolean;
  /** True while minting the token or probing the endpoint. */
  isLoading: boolean;
  /** Re-run the probe (e.g. after returning from the connect tab). */
  refetch: () => void;
  /** Opens the first-party connect page in a new tab, if a URL is available. */
  connect: () => void;
  /** Whether a connect URL could be derived (drives button enablement). */
  canConnect: boolean;
}

/**
 * Connection status for an issuer-gated playground toolset, the toolset-side
 * counterpart to the remote-MCP Tools tab (see RemoteMcpToolsSection). It mints
 * a user-session JWT scoped to the toolset and probes `/mcp/{slug}` with it:
 * a successful `tools/list` means the upstream session is linked (`connected`),
 * a 401 means it isn't (`needsAuth`). This replaces the removed
 * `/oauth-external/status` endpoint as the connected/not-connected signal.
 *
 * Both the auth panel (PlaygroundAuth) and the chat surface (PlaygroundElements)
 * call this; React Query dedupes the mint and the probe by their query keys, so
 * the two consumers share a single token and a single probe request.
 */
export function usePlaygroundIssuerConnection(
  toolset: Toolset | undefined,
): PlaygroundIssuerConnection {
  const isIssuerGated = !!toolset?.userSessionIssuerSlug;

  const { accessToken, isLoading: isTokenLoading } = useToolsetUserSessionToken(
    {
      toolsetId: toolset?.id,
      isIssuerGated,
    },
  );

  const mcpUrl = useInternalMcpUrl(toolset);
  // Connect through the dev proxy origin (same-origin) so the AI SDK transport
  // carries the gram_session cookie; no-op in prod. Mirrors the remote Tools tab.
  const connectUrl = useMemo(() => mcpConnectionUrl(mcpUrl), [mcpUrl]);

  const headers = useMemo(
    () =>
      accessToken ? { Authorization: `Bearer ${accessToken}` } : undefined,
    [accessToken],
  );

  // Issuer-gated toolsets must wait for the JWT before probing, otherwise the
  // unauthenticated request 401s and caches a spurious `needsAuth`.
  const probeEnabled = isIssuerGated && !!accessToken;

  const { tools, isLoading, needsAuth, refetch } = useRemoteMcpTools(
    connectUrl,
    {
      headers,
      enabled: probeEnabled,
    },
  );

  // The connect page is opened as a top-level tab on the toolset `/mcp` surface
  // so it rides the gram_session cookie on the backend origin (not the proxy).
  const authUrl = useMemo(
    () => firstPartyConnectUrl(mcpUrl, { runtimePath: "mcp" }),
    [mcpUrl],
  );

  const connect = useCallback(() => {
    if (authUrl) window.open(authUrl, "_blank", "noopener,noreferrer");
  }, [authUrl]);

  return {
    isIssuerGated,
    accessToken,
    connected: probeEnabled && !!tools && !needsAuth,
    needsAuth,
    isLoading: isIssuerGated && (isTokenLoading || isLoading),
    refetch,
    connect,
    canConnect: !!authUrl,
  };
}
