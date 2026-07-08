import { useSdkClient } from "@/contexts/Sdk";
import type { Toolset } from "@/lib/toolTypes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllListToolsets } from "@gram/client/react-query/listToolsets.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import type { QueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

// AuthTarget abstracts the resource a user_session_issuer gets linked to. The
// authentication components (section body, attach sheet, sessions list) only
// ever talk to this interface; the two implementations below know whether the
// link lives on mcp_servers.user_session_issuer_id or
// toolsets.user_session_issuer_id.
export type AuthTarget = {
  // Slug seeding auto-derived user_session_issuer / remote_session_issuer
  // slugs on first add.
  slug: string;
  // Current user_session_issuer link; null when the target has none yet.
  userSessionIssuerId: string | null;
  // Remote MCP server id for the RFC 9728 protected-resource probe. Undefined
  // when the target has no probeable upstream (tunneled and toolset-backed
  // servers), which leaves the probe idle.
  remoteMcpServerId?: string;
  // Link a freshly created user_session_issuer to the target (first add).
  linkUserSessionIssuer: (userSessionIssuerId: string) => Promise<void>;
  // Invalidate the target-specific queries that embed the link.
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
        // Point the MCP server at the user_session_issuer and set visibility
        // to private so the server begins serving traffic. updateMcpServer is
        // a full-record replace, so re-send the existing UUID references
        // alongside the update.
        await client.mcpServers.update({
          updateMcpServerForm: {
            id: mcpServer.id,
            name: mcpServer.name ?? undefined,
            remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
            tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
            toolsetId: mcpServer.toolsetId ?? undefined,
            environmentId: mcpServer.environmentId ?? undefined,
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
        // Toolset-backed servers are already live, so linking only flips auth
        // gating — mcpEnabled / mcpIsPublic stay untouched.
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
