import { asTool } from "@/lib/toolTypes";
import { useToolset as useToolsetQuery } from "@gram/client/react-query/toolset.js";
import { useListTools as useListToolsQuery } from "@gram/client/react-query/listTools.js";
import { ListToolsRequest } from "@gram/client/models/operations";

export function useToolset(
  toolsetSlug: string | undefined
) {
  const result = useToolsetQuery({ slug: toolsetSlug! }, undefined, {
    enabled: !!toolsetSlug,
  });

  return {
    ...result,
    data: result.data ? {
      ...result.data,
      tools: result.data.tools.map(asTool),
    } : undefined,
  };
}

export function useListTools(props?: ListToolsRequest) {
  const result = useListToolsQuery(props);

  return {
    ...result,
    data: result.data ? {
      ...result.data,
      tools: result.data.tools.map(asTool),
    } : undefined,
  };
} 
