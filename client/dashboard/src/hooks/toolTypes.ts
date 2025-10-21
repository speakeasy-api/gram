import { asResource, asTool } from "@/lib/toolTypes";
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
  ListResourcesQueryData,
  useListResources as useListResourcesQuery,
} from "@gram/client/react-query/listResources.js";
import {
  ListToolsQueryData,
  useListTools as useListToolsQuery,
} from "@gram/client/react-query/listTools.js";
import { useToolset as useToolsetQuery } from "@gram/client/react-query/toolset.js";

export function useToolset(toolsetSlug: string | undefined) {
  const result = useToolsetQuery({ slug: toolsetSlug! }, undefined, {
    enabled: !!toolsetSlug,
  });

  return {
    ...result,
    data: result.data
      ? {
          ...result.data,
          tools: result.data.tools.map(asTool),
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
