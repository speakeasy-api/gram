import { useSdkClient } from "@/contexts/Sdk";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import {
  linkMcpServerUserSessionIssuer,
  linkToolsetUserSessionIssuer,
} from "@/lib/linkUserSessionIssuer";
import type { Toolset } from "@/lib/toolTypes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { invalidateAllListToolsets } from "@gram/client/react-query/listToolsets.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
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
  /** Project owning the target. */
  projectId: string;
  /** Selects the Remote MCP identity surface without changing shared auth UI. */
  kind: "remote-mcp" | "standard";
  /** Resource identifier used by the target's mcp:write check. */
  permissionResourceId: string;
  /** The MCP server row id, when the target is one; differs from permissionResourceId for toolset-backed servers. */
  mcpServerId?: string;
  /** Whether the target backend accepts organization-owned issuers. */
  supportsOrganizationIssuers: boolean;
  /** Current issuer link; null when the target has none yet. */
  userSessionIssuerId: string | null;
  /**
   * Remote MCP server id for the RFC 9728 probe. Undefined for targets with
   * no probeable upstream (tunneled, toolset-backed), leaving the probe idle.
   */
  remoteMcpServerId?: string;
  /** Attach an existing or freshly created issuer to the target. */
  linkUserSessionIssuer?: (userSessionIssuerId: string) => Promise<void>;
  /** Names the target to the attach impact preview, which reports what
   * changing its issuer does to its connected clients. */
  attachImpactTarget: {
    mcpServerId?: string;
    toolsetId?: string;
    metaMcpServerId?: string;
  };
  /** The URL clients connect to, which a shared authorization server takes as
   * the server's resource. Undefined while it is unknown. */
  mcpUrl?: string;
  /** True when the target's issuer may bind several upstream providers
   * (gateways front many members). Remote/tunneled servers have exactly one
   * upstream and must not offer additional attachments. */
  multipleProviders?: boolean;
  /** Invalidate the target-specific queries that embed the link. */
  invalidate: (queryClient: QueryClient) => Promise<void>;
};

export function useMcpServerAuthTarget(
  mcpServer: McpServer,
  mcpUrl?: string,
): AuthTarget {
  const client = useSdkClient();

  return useMemo(
    () => ({
      slug: mcpServer.slug ?? "mcp",
      projectId: mcpServer.projectId,
      kind: mcpServer.remoteMcpServerId ? "remote-mcp" : "standard",
      permissionResourceId: mcpServer.toolsetId ?? mcpServer.id,
      mcpServerId: mcpServer.id,
      supportsOrganizationIssuers: true,
      userSessionIssuerId: mcpServer.userSessionIssuerId ?? null,
      attachImpactTarget: { mcpServerId: mcpServer.id },
      mcpUrl,
      remoteMcpServerId: mcpServer.remoteMcpServerId,
      linkUserSessionIssuer: (userSessionIssuerId: string) =>
        linkMcpServerUserSessionIssuer(
          client,
          mcpServer.id,
          userSessionIssuerId,
        ),
      invalidate: async (queryClient: QueryClient) => {
        await Promise.all([
          invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
          invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        ]);
      },
    }),
    [client, mcpServer, mcpUrl],
  );
}

export function useToolsetAuthTarget(toolset: Toolset): AuthTarget {
  const client = useSdkClient();
  const { url, customServerURL } = useMcpUrl(toolset);
  // A custom-domain URL takes the platform host until the domain resolves;
  // that address is not the one clients use, so leave it unknown until then.
  const mcpUrl =
    toolset.customDomainId && toolset.mcpSlug && !customServerURL
      ? undefined
      : url;

  return useMemo(
    () => ({
      slug: toolset.slug,
      projectId: toolset.projectId,
      kind: "standard",
      permissionResourceId: toolset.id,
      supportsOrganizationIssuers: true,
      userSessionIssuerId: toolset.userSessionIssuerId ?? null,
      attachImpactTarget: { toolsetId: toolset.id },
      mcpUrl,
      linkUserSessionIssuer: (userSessionIssuerId: string) =>
        linkToolsetUserSessionIssuer(client, toolset, userSessionIssuerId),
      invalidate: async (queryClient: QueryClient) => {
        await Promise.all([
          invalidateAllToolset(queryClient, { refetchType: "all" }),
          invalidateAllListToolsets(queryClient, { refetchType: "all" }),
          invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        ]);
      },
    }),
    [client, toolset, mcpUrl],
  );
}

export function useMetaMcpAuthTarget(
  metaMcpServer: MetaMcpServer,
  slugSeed: string,
  mcpUrl?: string,
): AuthTarget {
  const client = useSdkClient();

  return useMemo(
    () => ({
      slug: slugSeed,
      projectId: metaMcpServer.projectId,
      kind: "standard",
      permissionResourceId: metaMcpServer.projectId,
      supportsOrganizationIssuers: false,
      userSessionIssuerId: metaMcpServer.userSessionIssuerId ?? null,
      attachImpactTarget: { metaMcpServerId: metaMcpServer.id },
      mcpUrl,
      multipleProviders: true,
      linkUserSessionIssuer: async (userSessionIssuerId: string) => {
        // update is a full-record replace, so the name rides along.
        await client.metaMcp.update({
          updateMetaMcpServerForm: {
            id: metaMcpServer.id,
            name: metaMcpServer.name,
            userSessionIssuerId,
          },
        });
      },
      invalidate: async (queryClient: QueryClient) => {
        await Promise.all([
          invalidateAllGetMetaMcpServer(queryClient, { refetchType: "all" }),
          invalidateAllMetaMcpServers(queryClient, { refetchType: "all" }),
        ]);
      },
    }),
    [client, metaMcpServer, slugSeed, mcpUrl],
  );
}
