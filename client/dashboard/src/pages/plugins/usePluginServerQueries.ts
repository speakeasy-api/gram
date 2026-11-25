import { usePluginQueryScope } from "./usePluginQueryScope";
import { useProject } from "@/contexts/Auth";
import { hasScopeInGrants, useRBAC } from "@/hooks/useRBAC";
import { useListToolsets } from "@gram/client/react-query/listToolsets";
import { useMcpServers } from "@gram/client/react-query/mcpServers";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints";

export function usePluginServerQueries(): {
  canReadServers: boolean;
  toolsetsQuery: Pick<
    ReturnType<typeof useListToolsets>,
    "data" | "isLoading" | "error" | "refetch"
  >;
  serversQuery: Pick<
    ReturnType<typeof useMcpServers>,
    "data" | "isLoading" | "error" | "refetch"
  >;
  endpointsQuery: Pick<
    ReturnType<typeof useMcpEndpoints>,
    "data" | "isLoading" | "error" | "refetch"
  >;
} {
  const project = useProject();
  const scope = usePluginQueryScope();
  const { grants, isLoading } = useRBAC();
  // Toolsets/servers filter by accessible MCP resources. Endpoint listing
  // instead requires MCP read on the project itself. Org read implies neither.
  const canReadServers =
    !isLoading && hasScopeInGrants(grants, "mcp:read", undefined, project.id);
  const canReadEndpoints =
    !isLoading && hasScopeInGrants(grants, "mcp:read", project.id, project.id);
  const toolsetsQuery = useListToolsets(scope, undefined, {
    enabled: canReadServers,
  });
  const serversQuery = useMcpServers(scope, undefined, {
    enabled: canReadServers,
  });
  const endpointsQuery = useMcpEndpoints(scope, undefined, {
    enabled: canReadEndpoints,
  });
  // Disabled queries can still expose a previous identity's cache. Never pass
  // that metadata (or a stale error that would offer a retry) to the page or
  // picker after authorization is lost.
  return {
    canReadServers,
    toolsetsQuery: canReadServers
      ? toolsetsQuery
      : { ...toolsetsQuery, data: undefined, error: null },
    serversQuery: canReadServers
      ? serversQuery
      : { ...serversQuery, data: undefined, error: null },
    endpointsQuery: canReadEndpoints
      ? endpointsQuery
      : { ...endpointsQuery, data: undefined, error: null },
  };
}
