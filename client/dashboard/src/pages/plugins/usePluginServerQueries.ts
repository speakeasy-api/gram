import { useProject } from "@/contexts/Auth";
import { hasScopeInGrants, useRBAC } from "@/hooks/useRBAC";
import { useListToolsets } from "@gram/client/react-query/listToolsets";
import { useMcpServers } from "@gram/client/react-query/mcpServers";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints";

export function usePluginServerQueries(): {
  canReadServers: boolean;
  toolsetsQuery: ReturnType<typeof useListToolsets>;
  serversQuery: ReturnType<typeof useMcpServers>;
  endpointsQuery: ReturnType<typeof useMcpEndpoints>;
} {
  const project = useProject();
  const { grants } = useRBAC();
  // Toolsets/servers filter by accessible MCP resources. Endpoint listing
  // instead requires MCP read on the project itself. Org read implies neither.
  const canReadServers = hasScopeInGrants(
    grants,
    "mcp:read",
    undefined,
    project.id,
  );
  const canReadEndpoints = hasScopeInGrants(
    grants,
    "mcp:read",
    project.id,
    project.id,
  );
  const toolsetsQuery = useListToolsets(
    { gramProject: project.id },
    undefined,
    {
      enabled: canReadServers,
    },
  );
  const serversQuery = useMcpServers({ gramProject: project.id }, undefined, {
    enabled: canReadServers,
  });
  const endpointsQuery = useMcpEndpoints(
    { gramProject: project.id },
    undefined,
    {
      enabled: canReadEndpoints,
    },
  );
  return { canReadServers, toolsetsQuery, serversQuery, endpointsQuery };
}
