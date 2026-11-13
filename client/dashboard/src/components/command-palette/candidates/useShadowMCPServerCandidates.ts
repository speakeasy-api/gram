import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { shadowAITabSearch } from "@/pages/shadow-ai/tabs";
import { useRoutes } from "@/routes";
import { buildShadowMCPInventoryQuery } from "@gram/client/react-query/shadowMCPInventory.core.js";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

const INVENTORY_PAGE_SIZE = 200;

/**
 * The project's Shadow MCP inventory: every MCP server its agents used or
 * asked for, so a server name opens its page on Shadow AI. A local command
 * has no page; it opens the MCP servers tab, where its row reviews it.
 * Org-admin only, the same gate as the Shadow AI page; the caller gates
 * `enabled`.
 */
export function useShadowMCPServerCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const organization = useOrganization();
  const project = useProject();
  const sdk = useSdkClient();
  const routes = useRoutes();
  const navigate = useNavigate();
  const projectId = project.id;
  const queryEnabled = enabled && projectId.length > 0;
  const inventory = useInfiniteQuery({
    // The project and organization keep one tenant's cache out of another's.
    queryKey: [
      "command-palette",
      "shadow-mcp-inventory",
      organization.id,
      projectId,
    ],
    queryFn: (ctx) =>
      buildShadowMCPInventoryQuery(sdk, {
        projectId,
        limit: INVENTORY_PAGE_SIZE,
        cursor: ctx.pageParam,
      }).queryFn(ctx),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.nextCursor || undefined,
    enabled: queryEnabled,
    retry: false,
    throwOnError: false,
  });
  const { hasNextPage, isFetching, isFetchNextPageError, fetchNextPage } =
    inventory;

  // The palette has no pagination UI. Drain the cursor while it is open so a
  // server stays searchable whichever page holds it. Wait for any fetch in
  // flight, including the refetch of cached pages when the palette reopens,
  // so pages never load concurrently. After a failed page, stop rather than
  // retry in a render loop: the next open refetches, which clears the error
  // and resumes draining.
  useEffect(() => {
    if (!queryEnabled || !hasNextPage || isFetching || isFetchNextPageError) {
      return;
    }
    void fetchNextPage().catch(() => undefined);
  }, [
    queryEnabled,
    hasNextPage,
    isFetching,
    isFetchNextPageError,
    fetchNextPage,
  ]);

  return useMemo(() => {
    if (!queryEnabled) return [];
    const servers = inventory.data?.pages.flatMap((page) => page.servers) ?? [];
    return servers.map((server): LauncherCandidate => {
      const isLocalCommand = server.targetKind === "stdio_command";
      const href = isLocalCommand
        ? `${routes.shadowAI.href()}${shadowAITabSearch("mcp-servers")}`
        : routes.shadowAI.mcps.detail.href(server.serverSlug);
      return {
        id: `shadow_mcp_server:${server.serverSlug}`,
        kind: "shadow_mcp_server",
        // A local command known only from a request has no name or host.
        title: server.serverName || server.urlHost || server.canonicalServerUrl,
        detail: isLocalCommand ? "Shadow MCP · local command" : "Shadow MCP",
        // Server URLs and commands are data typed by agents' users; they are
        // matched locally and never sent anywhere.
        keywords: [
          "shadow",
          "mcp",
          server.serverSlug,
          server.urlHost,
          server.canonicalServerUrl,
        ],
        verbs: ["open"],
        icon: "eye-off",
        group: "Shadow MCP Servers",
        run: () => {
          void navigate(href);
        },
      };
    });
  }, [queryEnabled, inventory.data, routes, navigate]);
}
