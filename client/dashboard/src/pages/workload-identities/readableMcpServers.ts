import { useOrganization } from "@/contexts/Auth";
import { hasScopeInGrants, useRBAC } from "@/hooks/useRBAC";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useMemo } from "react";

export type ServerGroup = {
  projectId: string;
  projectName: string;
  servers: McpServer[];
};

// The connection details read requires mcp:read on the server, written against
// its toolset when it is toolset-backed, so only those servers are offered.
function canReadServer(
  grants: Parameters<typeof hasScopeInGrants>[0],
  server: McpServer,
): boolean {
  return hasScopeInGrants(
    grants,
    "mcp:read",
    server.toolsetId ?? server.id,
    server.projectId,
  );
}

export function serverLabel(server: McpServer): string {
  return server.name || server.slug || server.id;
}

type ReadableMcpServers = {
  groups: ServerGroup[];
  /** How many servers the organization has, readable or not. */
  total: number;
  isPending: boolean;
  isError: boolean;
  refetch: () => void;
};

/**
 * The organization's MCP servers the viewer can read, grouped by project.
 */
export function useReadableMcpServers(): ReadableMcpServers {
  const organization = useOrganization();
  const { grants, isLoading: grantsLoading } = useRBAC();
  const servers = useListMcpServersForOrg(undefined, undefined, {
    throwOnError: false,
  });

  const groups = useMemo((): ServerGroup[] => {
    const projectNames = new Map(
      organization.projects.map((project) => [project.id, project.name]),
    );
    const byProject = new Map<string, ServerGroup>();
    for (const server of servers.data?.mcpServers ?? []) {
      if (!canReadServer(grants, server)) continue;
      let group = byProject.get(server.projectId);
      if (!group) {
        group = {
          projectId: server.projectId,
          projectName: projectNames.get(server.projectId) ?? "Unknown project",
          servers: [],
        };
        byProject.set(server.projectId, group);
      }
      group.servers.push(server);
    }
    return [...byProject.values()];
  }, [grants, organization.projects, servers.data]);

  return {
    groups,
    total: servers.data?.mcpServers.length ?? 0,
    isPending: servers.isPending || grantsLoading,
    isError: servers.isError,
    refetch: () => void servers.refetch(),
  };
}

/**
 * The selection, while the server is still in the readable list, so losing
 * access to it hides its values rather than leaving them on screen.
 */
export function readableSelection(
  groups: ServerGroup[],
  selected: string,
): string {
  return groups.some((group) =>
    group.servers.some((server) => server.id === selected),
  )
    ? selected
    : "";
}
