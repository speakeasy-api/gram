import {
  useProxiedMcpTools,
  type ProxiedMcpTool,
} from "@/hooks/useProxiedMcpTools";
import {
  useToolMetadata,
  type ToolMetadataByName,
} from "@/hooks/useToolMetadata";
import { useUserSessionToken } from "@/hooks/useUserSessionToken";
import {
  firstPartyConnectUrl,
  getServerURL,
  mcpConnectionUrl,
  supportsFirstPartyConnect,
} from "@/lib/utils";
import { useEffect, useMemo } from "react";

import { useSyncToolMetadata } from "./useSyncToolMetadata";

export interface RemoteMcpToolConnectionOptions {
  /** The Gram-proxied MCP URL to connect to; undefined while it resolves. */
  mcpUrl: string | undefined;
  /** The mcp_server id, used to mint the user-session JWT. */
  mcpServerId: string | undefined;
  /** The server's user_session_issuer id; also the issuer-gated flag. */
  userSessionIssuerId: string | undefined;
  /** Only remote-backed servers carry stored tool metadata. */
  remoteMcpServerId: string | undefined;
  /** Gram-origin endpoint slug, for the first-party connect page. */
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
  /**
   * Opens the first-party connect page in a new tab; returning to this one
   * lists the tools again. Undefined when the server has no connect page.
   */
  connect: (() => void) | undefined;
  /** Make the stored tool metadata mirror the session. */
  sync: () => void;
  isSyncing: boolean;
}

/**
 * Connects to a remote MCP server through Gram to list its tools, and records
 * the tools it sees for the first time so they can be permissioned by name.
 *
 * Issuer-gated servers connect with a minted user-session JWT. With no
 * upstream session yet the listing comes back `needsAuth`, and `connect` opens
 * the first-party connect page; coming back to the tab tries again. Shared by
 * the Inspect tab and the role editor's tool access sheet.
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
}: RemoteMcpToolConnectionOptions): RemoteMcpToolConnection {
  const isIssuerGated = !!userSessionIssuerId;
  const canFirstPartyConnect = supportsFirstPartyConnect({
    userSessionIssuerId,
    platformSlug,
    visibility,
    tunneledMcpServerId,
  });

  const { accessToken, isLoading: isTokenLoading } = useUserSessionToken({
    target: { kind: "mcpServer", id: enabled ? mcpServerId : undefined },
    userSessionIssuerId,
    project,
  });

  // Toolset-backed servers carry no stored metadata, so skip the request.
  const { metadataByTool, isLoading: isMetadataLoading } = useToolMetadata(
    mcpServerId,
    { enabled: enabled && !!remoteMcpServerId, projectSlug: project?.slug },
  );

  // Issuer-gated servers must wait for the JWT before connecting, otherwise the
  // unauthenticated request 401s and caches a spurious `needsAuth`.
  const headers = useMemo(
    () =>
      accessToken ? { Authorization: `Bearer ${accessToken}` } : undefined,
    [accessToken],
  );
  const connectionEnabled = enabled && (!isIssuerGated || !!accessToken);

  // Connect through the dev proxy origin (same-origin) so the AI SDK transport
  // carries the gram_session cookie and the gateway's proxied SSE response
  // isn't dropped on a cross-origin hop. No-op in prod / for custom domains.
  const connectUrl = useMemo(() => mcpConnectionUrl(mcpUrl), [mcpUrl]);

  const { tools, isLoading, needsAuth, isError, refetch } = useProxiedMcpTools(
    connectUrl,
    { headers, enabled: connectionEnabled },
  );

  // The first-party connect page is opened as a top-level new tab, so it rides
  // the gram_session cookie on the backend origin (not the dev proxy). Built
  // from the platform slug only — the display URL may be a custom domain.
  const authUrl = useMemo(() => {
    if (!canFirstPartyConnect || !platformSlug) return undefined;
    return firstPartyConnectUrl(`${getServerURL()}/mcp/${platformSlug}`);
  }, [canFirstPartyConnect, platformSlug]);

  // When the user comes back from the connect tab, re-attempt the listing so a
  // freshly linked session surfaces without a manual refresh.
  useEffect(() => {
    if (!needsAuth) return;
    const onFocus = () => refetch();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [needsAuth, refetch]);

  const loading = !enabled || isTokenLoading || isLoading || isMetadataLoading;

  // Only remote-backed servers carry tool metadata, and there is nothing to
  // reconcile until both the session and the stored set have loaded.
  const { sync, isSyncing } = useSyncToolMetadata({
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
    connect: authUrl
      ? () => window.open(authUrl, "_blank", "noopener,noreferrer")
      : undefined,
    sync,
    isSyncing,
  };
}
