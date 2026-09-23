import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import {
  invalidateMcpServerQueries,
  mcpServerVisibilityToast,
  mcpServerVisibilityUpdateForm,
} from "@/lib/mcp-server-visibility";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { toast } from "sonner";
import type { LauncherCandidate, Verb } from "./types";

const GROUP = "MCP Servers";

function detailFor(server: McpServer): string {
  return server.visibility === "disabled"
    ? "MCP server · disabled"
    : "MCP server · enabled";
}

function verbsFor(server: McpServer, canWrite: boolean): Verb[] {
  if (!canWrite) return ["open"];
  return ["open", server.visibility === "disabled" ? "enable" : "disable"];
}

/**
 * MCP server candidates, mirroring the /mcp listing: toolset-backed (hosted)
 * servers from listToolsets and the non-toolset backends from mcpServers.
 * Toolset-backed mcp_servers rows are not listed on their own so hosted
 * servers aren't doubled (see the TODO(AGE-1902) note in the MCP page), but
 * they are the rows that carry a hosted server's visibility: each toolset
 * candidate reads its linked row's state and flips that row on
 * enable/disable. A toolset with no linked row is open-only.
 *
 * Verbs are gated per server (`hasScope("mcp:write", server.id)`), the same
 * check the server's own settings page applies, so a grant scoped to some
 * servers never attaches enable/disable to the others.
 */
export function useMcpServerCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const gramProject = useProjectSlugForRequests();
  // Both lists are keyed by project: the SDK folds gramProject into the query
  // key, so omitting it would share one cache entry across projects.
  const { data: toolsetsData } = useListToolsets({ gramProject }, undefined, {
    enabled,
  });
  const { data: mcpServersData } = useMcpServers({ gramProject }, undefined, {
    enabled,
  });
  const { mutateAsync: updateMcpServer } = useUpdateMcpServerMutation();

  return useMemo(() => {
    const toolsets = toolsetsData?.toolsets ?? [];
    const allServers = mcpServersData?.mcpServers ?? [];
    const servers = allServers.filter(
      (server) =>
        !!server.remoteMcpServerId ||
        !!server.tunneledMcpServerId ||
        !!server.unproxiedMcpServerId,
    );
    const byToolsetId = new Map<string, McpServer>();
    for (const server of allServers) {
      if (server.toolsetId && !byToolsetId.has(server.toolsetId)) {
        byToolsetId.set(server.toolsetId, server);
      }
    }
    const canWrite = (server: McpServer) => hasScope("mcp:write", server.id);

    const setVisibility = async (server: McpServer, verb: Verb) => {
      const visibility = verb === "enable" ? "private" : "disabled";
      try {
        await updateMcpServer({
          request: {
            updateMcpServerForm: mcpServerVisibilityUpdateForm(
              server,
              visibility,
            ),
          },
        });
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : "Failed to update MCP server",
        );
        throw error;
      }
      await invalidateMcpServerQueries(queryClient);
      toast.success(mcpServerVisibilityToast(visibility));
    };

    const toolsetCandidates: LauncherCandidate[] = toolsets.map((toolset) => {
      const linked = byToolsetId.get(toolset.id);
      return {
        id: `mcp:${toolset.id}`,
        kind: "mcp_server",
        title: toolset.name,
        detail: linked ? detailFor(linked) : "MCP server · enabled",
        keywords: ["mcp", toolset.slug, toolset.id],
        verbs: linked ? verbsFor(linked, canWrite(linked)) : ["open"],
        icon: "network",
        group: GROUP,
        run: (verb) => {
          if (linked && (verb === "enable" || verb === "disable")) {
            return setVisibility(linked, verb);
          }
          routes.mcp.details.goTo(toolset.slug);
        },
      };
    });

    const serverCandidates: LauncherCandidate[] = servers.map((server) => ({
      id: `mcp:${server.id}`,
      kind: "mcp_server",
      title: server.name || "MCP Server",
      detail: detailFor(server),
      keywords: ["mcp", server.slug ?? "", server.id].filter(Boolean),
      verbs: verbsFor(server, canWrite(server)),
      icon: "network",
      group: GROUP,
      run: (verb) => {
        if (verb === "enable" || verb === "disable") {
          return setVisibility(server, verb);
        }
        routes.mcp.x.overview.goTo(mcpServerRouteParam(server));
      },
    }));

    return [...toolsetCandidates, ...serverCandidates];
  }, [
    toolsetsData,
    mcpServersData,
    hasScope,
    routes,
    queryClient,
    updateMcpServer,
  ]);
}
