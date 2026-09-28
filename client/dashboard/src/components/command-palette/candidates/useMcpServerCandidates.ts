import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import {
  invalidateMcpServerQueries,
  mcpServerVisibilityToast,
  mcpServerVisibilityUpdateForm,
} from "@/lib/mcp-server-visibility";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { buildGetMcpServerQuery } from "@gram/client/react-query/getMcpServer.js";
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
 * Verbs are gated per server (`hasScope("mcp:write", <grant resource>)`),
 * the same check the server's own pages apply, so a grant scoped to some
 * servers never attaches enable/disable to the others. The grant resource is
 * the toolset id for a hosted server and the mcp_servers row id otherwise.
 */
export function useMcpServerCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const { hasScope } = useRBAC();
  const gramProject = useProjectSlugForRequests();
  // Both lists are keyed by project: the SDK folds gramProject into the query
  // key, so omitting it would share one cache entry across projects. Neither
  // throws: a failed list degrades to no candidates, not a blank palette.
  const { data: toolsetsData } = useListToolsets({ gramProject }, undefined, {
    enabled,
    throwOnError: false,
  });
  const { data: mcpServersData } = useMcpServers({ gramProject }, undefined, {
    enabled,
    throwOnError: false,
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
    // A hosted server is granted under its toolset id, not its wrapper row's
    // (the server resolves the grant the same way: grantResourceID in
    // server/internal/mcpservers/impl.go), so that is the id to check.
    const canWrite = (server: McpServer) =>
      hasScope("mcp:write", server.toolsetId ?? server.id);

    const setVisibility = async (server: McpServer, verb: Verb) => {
      const visibility = verb === "enable" ? "private" : "disabled";
      try {
        // The update replaces the whole record, so it is built from the row
        // as it is now rather than from the cached one the list rendered
        // (the same refetch the server's Danger Zone makes), so a stale
        // cache never overwrites configuration changed elsewhere.
        const latest = await queryClient.fetchQuery({
          ...buildGetMcpServerQuery(client, { id: server.id }),
          staleTime: 0,
        });
        if (latest.visibility !== visibility) {
          await updateMcpServer({
            request: {
              updateMcpServerForm: mcpServerVisibilityUpdateForm(
                latest,
                visibility,
              ),
            },
          });
        }
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
    client,
    updateMcpServer,
  ]);
}
