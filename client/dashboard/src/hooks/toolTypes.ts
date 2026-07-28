import { asResource, asTools } from "@/lib/toolTypes";
import { Tool as GeneratedTool } from "@gram/client/models/components/tool.js";
import {
  ListToolsRequest,
  ListToolsSecurity,
} from "@gram/client/models/operations/listtools.js";
import {
  ListResourcesRequest,
  ListResourcesSecurity,
} from "@gram/client/models/operations/listresources.js";
import { QueryHookOptions } from "@gram/client/react-query/_types.js";
import {
  LatestDeploymentQueryData,
  LatestDeploymentQueryError,
  useLatestDeployment as useLatestDeploymentQuery,
} from "@gram/client/react-query/latestDeployment.js";
import {
  ListResourcesQueryData,
  ListResourcesQueryError,
  useListResources as useListResourcesQuery,
} from "@gram/client/react-query/listResources.js";
import {
  ListToolsQueryData,
  ListToolsQueryError,
  useListTools as useListToolsQuery,
} from "@gram/client/react-query/listTools.js";
import {
  ToolsetQueryData,
  ToolsetQueryError,
  useToolset as useToolsetQuery,
} from "@gram/client/react-query/toolset.js";

type ToolsetKind = "default" | "external-mcp-proxy";

function detectToolsetKind(tools: GeneratedTool[]): ToolsetKind {
  const hasExternalMcpProxy = tools.some(
    (t) =>
      t.externalMcpToolDefinition !== undefined &&
      t.externalMcpToolDefinition.type === "proxy",
  );
  return hasExternalMcpProxy ? "external-mcp-proxy" : "default";
}

function useToolsetImpl(
  toolsetSlug: string | undefined,
  options?: QueryHookOptions<ToolsetQueryData, ToolsetQueryError>,
) {
  const result = useToolsetQuery({ slug: toolsetSlug! }, undefined, {
    ...options,
    enabled: !!toolsetSlug && (options?.enabled ?? true),
  });

  const kind = detectToolsetKind(result.data?.tools ?? []);
  const tools = asTools(result.data?.tools ?? []);

  return {
    ...result,
    data: result.data
      ? {
          ...result.data,
          kind,
          tools,
          rawTools: result.data.tools ?? [],
          resources: result.data.resources?.map(asResource) ?? [],
        }
      : undefined,
  };
}

export function useToolset(
  toolsetSlug: string | undefined,
  options?: QueryHookOptions<ToolsetQueryData, ToolsetQueryError>,
): ReturnType<typeof useToolsetImpl> {
  return useToolsetImpl(toolsetSlug, options);
}

function useListToolsImpl(
  request?: ListToolsRequest,
  security?: ListToolsSecurity,
  options?: QueryHookOptions<ListToolsQueryData, ListToolsQueryError>,
) {
  const result = useListToolsQuery(request, security, options);

  return {
    ...result,
    data: result.data
      ? {
          ...result.data,
          tools: asTools(result.data.tools),
        }
      : undefined,
  };
}

export function useListTools(
  request?: ListToolsRequest,
  security?: ListToolsSecurity,
  options?: QueryHookOptions<ListToolsQueryData, ListToolsQueryError>,
): ReturnType<typeof useListToolsImpl> {
  return useListToolsImpl(request, security, options);
}

function useListResourcesImpl(
  request?: ListResourcesRequest,
  security?: ListResourcesSecurity,
  options?: QueryHookOptions<ListResourcesQueryData, ListResourcesQueryError>,
) {
  const result = useListResourcesQuery(request, security, options);

  return {
    ...result,
    data: result.data
      ? {
          ...result.data,
          resources: result.data.resources.map(asResource),
        }
      : undefined,
  };
}

export function useListResources(
  request?: ListResourcesRequest,
  security?: ListResourcesSecurity,
  options?: QueryHookOptions<ListResourcesQueryData, ListResourcesQueryError>,
): ReturnType<typeof useListResourcesImpl> {
  return useListResourcesImpl(request, security, options);
}

/**
 * Hook for fetching the latest deployment with a 1-hour stale time.
 * Use this when you need deployment data that doesn't need to be
 * immediately fresh (e.g., asset lists, function metadata).
 */
export function useLatestDeployment(
  options?: QueryHookOptions<
    LatestDeploymentQueryData,
    LatestDeploymentQueryError
  >,
): ReturnType<typeof useLatestDeploymentQuery> {
  return useLatestDeploymentQuery(undefined, undefined, {
    staleTime: 1000 * 60 * 60, // 1 hour
    ...options,
  });
}
