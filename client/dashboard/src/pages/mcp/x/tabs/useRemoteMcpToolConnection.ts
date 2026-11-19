import {
  useProxiedMcpTools,
  type ProxiedMcpTool,
} from "@/hooks/useProxiedMcpTools";
import {
  useToolMetadata,
  type ToolMetadataByName,
} from "@/hooks/useToolMetadata";
import { useInspectOAuth } from "@/hooks/useInspectOAuth";
import {
  firstPartyConnectUrl,
  getServerURL,
  mcpConnectionUrl,
  supportsFirstPartyConnect,
} from "@/lib/utils";
import { useEffect, useMemo } from "react";

import { useChatSessionHeaders } from "../../inspect/useChatSessionHeaders";
import { useSyncToolMetadata } from "./useSyncToolMetadata";

export interface RemoteMcpToolConnectionOptions {
  /** The Speakeasy-proxied MCP URL to connect to; undefined while it resolves. */
  mcpUrl: string | undefined;
  /** The mcp_server id, used to scope the OAuth grant and metadata. */
  mcpServerId: string | undefined;
  /** The server's user_session_issuer id; also the issuer-gated flag. */
  userSessionIssuerId: string | undefined;
  /** Only remote-backed servers carry stored tool metadata. */
  remoteMcpServerId: string | undefined;
  /** Speakeasy-origin endpoint slug, for the first-party connect page. */
  platformSlug: string | undefined;
  tunneledMcpServerId: string | undefined;
  visibility: string | undefined;
  /**
   * The server's project, for org-level pages with no ambient project. Every
   * request that would otherwise read the project from the URL names it.
   */
  project?: { id: string; slug: string };
  /** False while the inputs above are still loading. */
  enabled?: boolean;
  /**
   * Authenticate with a dashboard chat session when there's no issuer, the
   * way hosted servers accept the chat. Off for callers that only list.
   */
  chatSession?: boolean;
  /** Use interactive OAuth in Inspect; the role editor keeps its draft in place. */
  inspectOAuth?: boolean;
  /** Legacy role editor preview, supplied by the role editor itself. */
  dashboardToken?: { accessToken: string | undefined; isLoading: boolean };
}

export interface RemoteMcpToolConnection {
  /** What the live session advertises; undefined until it has listed. */
  tools: Record<string, ProxiedMcpTool> | undefined;
  /** Speakeasy's stored annotations for the server's tools. */
  metadataByTool: ToolMetadataByName;
  loading: boolean;
  /** The listing was refused for missing or expired upstream credentials. */
  needsAuth: boolean;
  isError: boolean;
  isIssuerGated: boolean;
  refetch: () => void;
  /** Starts sign-in when this server supports it. */
  connect: (() => void) | undefined;
  /** Make the stored tool metadata mirror the session. */
  sync: () => void;
  isSyncing: boolean;
  /** This viewer can change the stored tool metadata. */
  canWrite: boolean;
  /** Where and how to send further MCP requests on this connection. */
  connectUrl: string | undefined;
  headers: Record<string, string> | undefined;
}

/**
 * Connects to a remote MCP server through Speakeasy to list its tools, and records
 * the tools it sees for the first time so they can be permissioned by name.
 *
 * Inspect uses a user-bound OAuth grant; the role editor still uses the
 * dashboard session so that a live preview cannot navigate away from a draft.
 */
export function useRemoteMcpToolConnection({
  mcpUrl,
  mcpServerId,
  userSessionIssuerId,
  remoteMcpServerId,
  platformSlug,
  tunneledMcpServerId,
  visibility,
  project,
  enabled = true,
  chatSession = false,
  inspectOAuth = false,
  dashboardToken,
}: RemoteMcpToolConnectionOptions): RemoteMcpToolConnection {
  // Public tunnels serve anonymously and intentionally have no OAuth routes.
  const publicTunnel =
    inspectOAuth && !!tunneledMcpServerId && visibility === "public";
  const isIssuerGated = !!userSessionIssuerId && !publicTunnel;
  const canFirstPartyConnect = supportsFirstPartyConnect({
    userSessionIssuerId,
    platformSlug,
    visibility,
    tunneledMcpServerId,
  });

  const oauth = useInspectOAuth({
    platformSlug:
      enabled && inspectOAuth && canFirstPartyConnect
        ? platformSlug
        : undefined,
    issuerId: userSessionIssuerId,
    serverId: mcpServerId,
  });
  const accessToken = inspectOAuth
    ? oauth.accessToken
    : dashboardToken?.accessToken;
  const endpointUrl = oauth.serverUrl ?? mcpUrl;

  // Toolset-backed servers carry no stored metadata, so skip the request.
  const { metadataByTool, isLoading: isMetadataLoading } = useToolMetadata(
    mcpServerId,
    { enabled: enabled && !!remoteMcpServerId, projectSlug: project?.slug },
  );

  // Issuer-gated servers must wait for the JWT before connecting, otherwise the
  // unauthenticated request 401s and caches a spurious `needsAuth`.
  const usesChatSession = chatSession && !isIssuerGated && !publicTunnel;
  const chat = useChatSessionHeaders(enabled && usesChatSession);
  const headers = useMemo(
    () =>
      accessToken
        ? { Authorization: `Bearer ${accessToken}` }
        : usesChatSession
          ? chat.headers
          : undefined,
    [accessToken, usesChatSession, chat.headers],
  );
  const connectionEnabled =
    enabled &&
    (isIssuerGated ? !!accessToken : !usesChatSession || !!chat.headers);

  // Connect through the dev proxy origin (same-origin) so the AI SDK transport
  // carries the gram_session cookie and the gateway's proxied SSE response
  // isn't dropped on a cross-origin hop. No-op in prod / for custom domains.
  const connectUrl = useMemo(
    () => mcpConnectionUrl(endpointUrl),
    [endpointUrl],
  );

  const {
    tools,
    isLoading,
    needsAuth: listingNeedsAuth,
    isError: isListError,
    refetch: refetchTools,
  } = useProxiedMcpTools(
    connectUrl,
    // Every failure stays inline as `isError`, so each consumer can offer its
    // own retry instead of an error boundary.
    { headers, enabled: connectionEnabled, throwOnError: false, inspect: true },
  );

  // The first-party connect page is opened as a top-level new tab, so it rides
  // the gram_session cookie on the backend origin (not the dev proxy). Built
  // from the platform slug only — the display URL may be a custom domain.
  const authUrl = useMemo(() => {
    if (inspectOAuth || !canFirstPartyConnect || !platformSlug)
      return undefined;
    return firstPartyConnectUrl(`${getServerURL()}/mcp/${platformSlug}`);
  }, [inspectOAuth, canFirstPartyConnect, platformSlug]);

  // A chat session that couldn't be created leaves nothing to connect with;
  // report it as a failed listing so the retry mints a new one. A failed
  // background refresh keeps the previous token, so it is not fatal.
  const chatFailed = usesChatSession && !!chat.error && !chat.headers;
  const needsAuth =
    listingNeedsAuth ||
    (enabled && inspectOAuth && isIssuerGated && !accessToken && !oauth.error);
  const isError = isListError || chatFailed || oauth.error;
  const refetch =
    oauth.error && oauth.connect
      ? oauth.connect
      : chatFailed
        ? chat.refetch
        : refetchTools;

  // When the user comes back from the connect tab, re-attempt the listing so a
  // freshly linked session surfaces without a manual refresh.
  useEffect(() => {
    if (!authUrl || !listingNeedsAuth) return;
    const onFocus = () => refetch();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [authUrl, listingNeedsAuth, refetch]);

  const loading =
    !enabled ||
    !!dashboardToken?.isLoading ||
    oauth.isConnecting ||
    chat.isLoading ||
    isLoading ||
    isMetadataLoading;

  // Only remote-backed servers carry tool metadata, and there is nothing to
  // reconcile until both the session and the stored set have loaded.
  const { sync, isSyncing, canWrite } = useSyncToolMetadata({
    mcpServerId,
    live: tools,
    stored: metadataByTool,
    enabled: !!remoteMcpServerId && !loading && !!tools,
    project,
  });

  return {
    tools,
    metadataByTool,
    loading,
    needsAuth,
    isError,
    isIssuerGated,
    refetch,
    connect: inspectOAuth
      ? oauth.connect
      : authUrl
        ? () => window.open(authUrl, "_blank", "noopener,noreferrer")
        : undefined,
    sync,
    isSyncing,
    canWrite,
    connectUrl,
    headers,
  };
}
