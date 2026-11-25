import { useInspectOAuth } from "@/hooks/useInspectOAuth";
import { useProxiedMcpTools } from "@/hooks/useProxiedMcpTools";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolMetadata } from "@/hooks/useToolMetadata";
import { getServerURL, mcpConnectionUrl } from "@/lib/utils";
import { useChatSessionHeaders } from "@/pages/mcp/inspect/useChatSessionHeaders";
import { computeDrift } from "@/pages/mcp/x/tabs/toolMetadataSync";
import { useMemo } from "react";

export type McpServerReachability =
  | { kind: "off" }
  | { kind: "checking" }
  /** No session could be made to ask the server, so it is not known. */
  | { kind: "unknown" }
  /** Issuer-gated and the viewer hasn't connected in Inspect, so not asked. */
  | { kind: "unverified" }
  /** The server answered; `driftCount` tools changed since the saved list. */
  | { kind: "reachable"; driftCount: number }
  | { kind: "unreachable"; detail: string | undefined };

/**
 * Whether a remote-backed server answers through Speakeasy, read from the
 * same tools/list query the Inspect tab makes. The query key, OAuth grant
 * and chat session match Inspect's, so the header and Inspect share one
 * listing. A refused sign-in still means the server answered.
 *
 * An issuer-gated server is only probed with the grant the viewer made by
 * connecting in Inspect. It never mints a session of its own: that would
 * record a client session for every viewer who merely opens the page.
 *
 * Unlike Inspect it never writes the stored tool metadata: it only reads the
 * listing and compares it with the saved list.
 */
export function useMcpServerReachability({
  mcpUrl,
  mcpServerId,
  projectId,
  userSessionIssuerId,
  platformSlug,
  publicTunnel,
  enabled,
}: {
  mcpUrl: string | undefined;
  mcpServerId: string | undefined;
  /** The server's project, which a project-scoped mcp:write grant names. */
  projectId: string | undefined;
  userSessionIssuerId: string | undefined;
  /** Speakeasy-origin endpoint slug; Inspect's grant is bound to its URL. */
  platformSlug: string | undefined;
  /** Public tunnels serve anonymously, as Inspect connects to them. */
  publicTunnel: boolean;
  /** False for servers that are off, still loading, or not remote-backed. */
  enabled: boolean;
}): McpServerReachability {
  const isIssuerGated = !!userSessionIssuerId;
  const requiresToken = isIssuerGated && !publicTunnel;
  // Same address as Inspect's, so the grant key matches the one it stored.
  const { accessToken, serverUrl } = useInspectOAuth({
    serverUrl:
      enabled && requiresToken
        ? platformSlug
          ? `${getServerURL()}/mcp/${encodeURIComponent(platformSlug)}`
          : mcpUrl
        : undefined,
    issuerId: userSessionIssuerId,
    serverId: mcpServerId,
  });
  const usesChatSession = !isIssuerGated && !publicTunnel;
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
  const connectUrl = useMemo(
    () => mcpConnectionUrl(serverUrl ?? mcpUrl),
    [serverUrl, mcpUrl],
  );
  const listing = useProxiedMcpTools(connectUrl, {
    headers,
    enabled:
      enabled &&
      (requiresToken ? !!accessToken : !usesChatSession || !!chat.headers),
    throwOnError: false,
    inspect: true,
  });

  // The drift Inspect offers to sync: only for viewers who can sync, and
  // only once a saved list exists.
  const { hasAnyScope } = useRBAC();
  const canWrite = hasAnyScope(["mcp:write"], mcpServerId, projectId);
  const { metadataByTool } = useToolMetadata(mcpServerId, { enabled });
  const driftCount = useMemo(
    () =>
      canWrite && listing.tools && Object.keys(metadataByTool).length > 0
        ? computeDrift(listing.tools, metadataByTool).length
        : 0,
    [canWrite, listing.tools, metadataByTool],
  );

  if (!enabled) return { kind: "off" };
  // Without the viewer's own grant there is nothing to ask the server with,
  // and a listing cached from an earlier grant doesn't speak for this one.
  if (requiresToken && !accessToken) return { kind: "unverified" };
  if (listing.tools) return { kind: "reachable", driftCount };
  if (listing.needsAuth) return { kind: "reachable", driftCount: 0 };
  if (listing.isError) {
    return { kind: "unreachable", detail: listing.error?.message };
  }
  // No session to list with says nothing about the server itself.
  if (usesChatSession && !!chat.error && !chat.headers) {
    return { kind: "unknown" };
  }
  return { kind: "checking" };
}
