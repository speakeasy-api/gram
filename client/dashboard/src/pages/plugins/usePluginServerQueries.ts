import { useProject } from "@/contexts/Auth";
import { hasScopeInGrants, useRBAC } from "@/hooks/useRBAC";
import { useListToolsets } from "@gram/client/react-query/listToolsets";
import { useMcpServers } from "@gram/client/react-query/mcpServers";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints";

export function usePluginServerQueries() {
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
  const toolsetsQuery = useListToolsets(undefined, undefined, {
    enabled: canReadServers,
  });
  const serversQuery = useMcpServers({}, undefined, {
    enabled: canReadServers,
  });
  const endpointsQuery = useMcpEndpoints({}, undefined, {
    enabled: canReadEndpoints,
  });
  return { toolsetsQuery, serversQuery, endpointsQuery };
}
