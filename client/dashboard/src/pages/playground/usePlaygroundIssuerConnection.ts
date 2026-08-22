import { useMcpConnectConsent } from "@/hooks/useMcpConnectConsent";
import { useProxiedMcpTools } from "@/hooks/useProxiedMcpTools";
import { useUserSessionToken } from "@/hooks/useUserSessionToken";
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
  /**
   * True while an issuer-gated toolset is waiting for the user's explicit
   * Connect click. Minting a user-session JWT persists a session row
   * server-side, so nothing is minted (and no probe runs) until the user
   * consents via `requestConnect` (or clicks `connect`).
   */
  needsExplicitConnect: boolean;
  /** Records the Connect consent for this toolset (persisted across visits). */
  requestConnect: () => void;
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
  const isIssuerGated = !!toolset?.userSessionIssuerId;

  // Issuer-gated toolsets only mint a user-session token after an explicit
  // Connect click — minting persists a session row, so opening the playground
  // alone must not establish one. The consent is persisted (keyed by toolset
  // id) so return visits reconnect without another click. Both consumers of
  // this hook share the same persisted flag, so consent recorded from either
  // surface unblocks the other.
  const { connectRequested, requestConnect } = useMcpConnectConsent(
    isIssuerGated ? toolset?.id : undefined,
  );
  const needsExplicitConnect = isIssuerGated && !connectRequested;

  const { accessToken, isLoading: isTokenLoading } = useUserSessionToken({
    target: { kind: "toolset", id: toolset?.id },
    userSessionIssuerId: toolset?.userSessionIssuerId,
    enabled: connectRequested,
  });

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

  const { tools, isLoading, needsAuth, refetch } = useProxiedMcpTools(
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

  // Opening the connect page is itself an explicit Connect action, so record
  // the consent too — otherwise a user who linked their upstream session from
  // the auth panel would still be blocked on the mint gate afterwards.
  const connect = useCallback(() => {
    requestConnect();
    if (authUrl) window.open(authUrl, "_blank", "noopener,noreferrer");
  }, [authUrl, requestConnect]);

  return {
    isIssuerGated,
    accessToken,
    connected: probeEnabled && !!tools && !needsAuth,
    needsAuth,
    isLoading: isIssuerGated && (isTokenLoading || isLoading),
    refetch,
    connect,
    canConnect: !!authUrl,
    needsExplicitConnect,
    requestConnect,
  };
}
