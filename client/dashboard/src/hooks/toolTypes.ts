import { asResource, asTool, Tool } from "@/lib/toolTypes";
import {
  ListToolsRequest,
  ListToolsSecurity,
} from "@gram/client/models/operations";
import {
  ListResourcesRequest,
  ListResourcesSecurity,
} from "@gram/client/models/operations/listresources.js";
import { QueryHookOptions } from "@gram/client/react-query";
import {
  LatestDeploymentQueryData,
  useLatestDeployment as useLatestDeploymentQuery,
} from "@gram/client/react-query/latestDeployment.js";
import {
  ListResourcesQueryData,
  useListResources as useListResourcesQuery,
} from "@gram/client/react-query/listResources.js";
import {
  ListToolsQueryData,
  useListTools as useListToolsQuery,
} from "@gram/client/react-query/listTools.js";
import { useToolset as useToolsetQuery } from "@gram/client/react-query/toolset.js";

export type ToolsetKind = "default" | "external-mcp";

function detectToolsetKind(tools: Tool[]): ToolsetKind {
  const hasExternalMcp = tools.some((t) => t.type === "external-mcp");
  return hasExternalMcp ? "external-mcp" : "default";
}

export function useToolset(toolsetSlug: string | undefined) {
  const result = useToolsetQuery({ slug: toolsetSlug! }, undefined, {
    enabled: !!toolsetSlug,
  });

  const tools = result.data?.tools.map(asTool) ?? [];
  const kind = detectToolsetKind(tools);

  return {
    ...result,
    data: result.data
      ? {
          ...result.data,
          kind,
          tools,
          resources: result.data.resources?.map(asResource) ?? [],
        }
      : undefined,
  };
}

export function useListTools(
  request?: ListToolsRequest,
  security?: ListToolsSecurity,
  options?: QueryHookOptions<ListToolsQueryData>,
) {
  const result = useListToolsQuery(request, security, options);

  return {
    ...result,
    data: result.data
      ? {
          ...result.data,
          tools: result.data.tools.map(asTool),
        }
      : undefined,
  };
}

export function useListResources(
  request?: ListResourcesRequest,
  security?: ListResourcesSecurity,
  options?: QueryHookOptions<ListResourcesQueryData>,
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

/**
 * Hook for fetching the latest deployment with a 1-hour stale time.
 * Use this when you need deployment data that doesn't need to be
 * immediately fresh (e.g., asset lists, function metadata).
 */
export function useLatestDeployment(
  options?: QueryHookOptions<LatestDeploymentQueryData>,
) {
  return useLatestDeploymentQuery(undefined, undefined, {
    staleTime: 1000 * 60 * 60, // 1 hour
    ...options,
  });
}
