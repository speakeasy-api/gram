import { useProxiedMcpTools } from "@/hooks/useProxiedMcpTools";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolMetadata } from "@/hooks/useToolMetadata";
import { useUserSessionToken } from "@/hooks/useUserSessionToken";
import { mcpConnectionUrl } from "@/lib/utils";
import { useChatSessionHeaders } from "@/pages/mcp/inspect/useChatSessionHeaders";
import { computeDrift } from "@/pages/mcp/x/tabs/toolMetadataSync";
import { useMemo } from "react";

export type McpServerReachability =
  | { kind: "off" }
  | { kind: "checking" }
  /** No session could be made to ask the server, so it is not known. */
  | { kind: "unknown" }
  /** The server answered; `driftCount` tools changed since the saved list. */
  | { kind: "reachable"; driftCount: number }
  | { kind: "unreachable"; detail: string | undefined };

/**
 * Whether a remote-backed server answers through Speakeasy, read from the
 * same tools/list query the Inspect tab makes. The query key, session token
 * and chat session match Inspect's, so the header and Inspect share one
 * listing. A refused sign-in still means the server answered.
 *
 * Unlike Inspect it never writes the stored tool metadata: it only reads the
 * listing and compares it with the saved list.
 */
export function useMcpServerReachability({
  mcpUrl,
  mcpServerId,
  projectId,
  userSessionIssuerId,
  enabled,
}: {
  mcpUrl: string | undefined;
  mcpServerId: string | undefined;
  /** The server's project, which a project-scoped mcp:write grant names. */
  projectId: string | undefined;
  userSessionIssuerId: string | undefined;
  /** False for servers that are off, still loading, or not remote-backed. */
  enabled: boolean;
}): McpServerReachability {
  const isIssuerGated = !!userSessionIssuerId;
  const { accessToken, isError: tokenFailed } = useUserSessionToken({
    target: { kind: "mcpServer", id: enabled ? mcpServerId : undefined },
    userSessionIssuerId,
    throwOnError: false,
  });
  const chat = useChatSessionHeaders(enabled && !isIssuerGated);
  const headers = useMemo(
    () =>
      accessToken
        ? { Authorization: `Bearer ${accessToken}` }
        : !isIssuerGated
          ? chat.headers
          : undefined,
    [accessToken, isIssuerGated, chat.headers],
  );
  const connectUrl = useMemo(() => mcpConnectionUrl(mcpUrl), [mcpUrl]);
  const listing = useProxiedMcpTools(connectUrl, {
    headers,
    enabled: enabled && (isIssuerGated ? !!accessToken : !!chat.headers),
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
  if (listing.tools) return { kind: "reachable", driftCount };
  if (listing.needsAuth) return { kind: "reachable", driftCount: 0 };
  if (listing.isError) {
    return { kind: "unreachable", detail: listing.error?.message };
  }
  // No session to list with says nothing about the server itself.
  if (isIssuerGated ? tokenFailed : !!chat.error && !chat.headers) {
    return { kind: "unknown" };
  }
  return { kind: "checking" };
}
