import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { useMemo } from "react";

export interface ProjectServerIds {
  /** Every id a rule can name one of the project's servers by. */
  serverIds: string[];
  /** Every id the given server is granted under. */
  resourceIds: string[];
  /** How many of the project's servers there are besides the given one. */
  otherServerCount: number;
  /** Whether both listings have loaded, so the ids are complete. */
  settled: boolean;
}

/**
 * The ids a rule naming one of a project's servers can carry. Serving checks
 * the toolset id for a toolset-backed server and the mcp_servers id for a
 * remote or tunneled one (see GRANT ID INVARIANT in pages/access/serverMerge),
 * so both are collected. Unlike the grant pickers nothing is filtered out:
 * replacing a rule covering every server must not drop a server the pickers
 * hide because it has no tools yet.
 */
export function useProjectServerIds(
  projectId: string,
  resourceId: string,
  enabled: boolean,
): ProjectServerIds {
  const toolsets = useListToolsetsForOrg(undefined, undefined, {
    enabled,
    throwOnError: false,
  });
  const mcpServers = useListMcpServersForOrg(undefined, undefined, {
    enabled,
    throwOnError: false,
  });

  const ids = useMemo(() => {
    const serverIds = new Set<string>();
    const resourceIds = new Set<string>([resourceId]);
    // One key per server: a toolset-backed server appears in both listings
    // and is counted once, under its toolset id.
    const servers = new Set<string>();
    for (const toolset of toolsets.data?.toolsets ?? []) {
      if (toolset.projectId !== projectId) continue;
      serverIds.add(toolset.id);
      servers.add(toolset.id);
    }
    for (const server of mcpServers.data?.mcpServers ?? []) {
      if (server.projectId !== projectId) continue;
      serverIds.add(server.id);
      servers.add(server.toolsetId ?? server.id);
      if (server.toolsetId) serverIds.add(server.toolsetId);
      // The server being cleared may be named by either of its ids.
      if (server.id === resourceId || server.toolsetId === resourceId) {
        resourceIds.add(server.id);
        if (server.toolsetId) resourceIds.add(server.toolsetId);
      }
    }
    const otherServerCount = [...servers].filter(
      (key) => !resourceIds.has(key),
    ).length;
    return {
      serverIds: [...serverIds],
      resourceIds: [...resourceIds],
      otherServerCount,
    };
  }, [toolsets.data, mcpServers.data, projectId, resourceId]);

  return {
    ...ids,
    settled: toolsets.isSuccess && mcpServers.isSuccess,
  };
}
