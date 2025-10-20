import { asResource, asTool } from "@/lib/toolTypes";
import {
  ListToolsRequest,
  ListToolsSecurity,
} from "@gram/client/models/operations";
import { QueryHookOptions } from "@gram/client/react-query";
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
