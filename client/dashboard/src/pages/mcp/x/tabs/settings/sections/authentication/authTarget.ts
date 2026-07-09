import { useSdkClient } from "@/contexts/Sdk";
import type { Toolset } from "@/lib/toolTypes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllListToolsets } from "@gram/client/react-query/listToolsets.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import type { QueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

/**
 * Abstracts the resource a user_session_issuer links to. The auth components
 * (section body, attach sheet, sessions list) only talk to this interface;
 * the implementations below know whether the link lives on
 * mcp_servers.user_session_issuer_id or toolsets.user_session_issuer_id.
 */
export type AuthTarget = {
  /** Seeds auto-derived issuer slugs on first add. */
  slug: string;
  /** Current issuer link; null when the target has none yet. */
  userSessionIssuerId: string | null;
  /**
   * Remote MCP server id for the RFC 9728 probe. Undefined for targets with
   * no probeable upstream (tunneled, toolset-backed), leaving the probe idle.
   */
  remoteMcpServerId?: string;
  /** Link a freshly created issuer to the target (first add). */
  linkUserSessionIssuer: (userSessionIssuerId: string) => Promise<void>;
  /** Invalidate the target-specific queries that embed the link. */
  invalidate: (queryClient: QueryClient) => Promise<void>;
};

export function useMcpServerAuthTarget(mcpServer: McpServer): AuthTarget {
  const client = useSdkClient();

  return useMemo(
    () => ({
      slug: mcpServer.slug ?? "mcp",
      userSessionIssuerId: mcpServer.userSessionIssuerId ?? null,
      remoteMcpServerId: mcpServer.remoteMcpServerId,
      linkUserSessionIssuer: async (userSessionIssuerId: string) => {
        // Point the server at the issuer and set visibility private so it
        // serves traffic. update is a full-record replace, so re-send the
        // existing UUID references alongside.
        await client.mcpServers.update({
          updateMcpServerForm: {
            id: mcpServer.id,
            name: mcpServer.name ?? undefined,
            remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
            tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
            toolsetId: mcpServer.toolsetId ?? undefined,
            environmentId: mcpServer.environmentId ?? undefined,
            toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
            visibility: "private",
            userSessionIssuerId,
          },
        });
      },
      invalidate: async (queryClient: QueryClient) => {
        await Promise.all([
          invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
          invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        ]);
      },
    }),
    [client, mcpServer],
  );
}

export function useToolsetAuthTarget(toolset: Toolset): AuthTarget {
  const client = useSdkClient();

  return useMemo(
    () => ({
      slug: toolset.slug,
      userSessionIssuerId: toolset.userSessionIssuerId ?? null,
      linkUserSessionIssuer: async (userSessionIssuerId: string) => {
        // Toolsets are already live, so linking only flips auth gating —
        // mcpEnabled / mcpIsPublic stay untouched.
        await client.toolsets.setUserSessionIssuer({
          slug: toolset.slug,
          setUserSessionIssuerRequestBody: { userSessionIssuerId },
        });
      },
      invalidate: async (queryClient: QueryClient) => {
        await Promise.all([
          invalidateAllToolset(queryClient, { refetchType: "all" }),
          invalidateAllListToolsets(queryClient, { refetchType: "all" }),
        ]);
      },
    }),
    [client, toolset],
  );
}
