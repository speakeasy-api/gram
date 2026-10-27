import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useToolset } from "@/hooks/toolTypes";
import type { Toolset } from "@/lib/toolTypes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";

/** The full toolset a hosted server serves; pass undefined for any other server. */
export function useHostedToolset(server: McpServer | undefined): {
  toolset: Toolset | undefined;
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
} {
  const gramProject = useProjectSlugForRequests();
  const summarySlug = server?.toolset?.slug;
  // The summary is absent when its read failed; find the slug from the list instead.
  const {
    data: toolsetsResult,
    isLoading: isLoadingList,
    isError: listFailed,
    refetch: refetchList,
  } = useListToolsets({ gramProject }, undefined, {
    enabled: server !== undefined && !summarySlug,
    throwOnError: false,
  });
  const slug =
    summarySlug ??
    toolsetsResult?.toolsets.find((t) => t.id === server?.toolsetId)?.slug;
  const {
    data: toolset,
    isLoading: isLoadingToolset,
    isError,
    refetch,
  } = useToolset(slug, {
    enabled: slug !== undefined,
    throwOnError: false,
  });
  return {
    toolset: slug ? toolset : undefined,
    isLoading:
      server !== undefined &&
      ((!summarySlug && isLoadingList) || (!!slug && isLoadingToolset)),
    isError: isError || (!summarySlug && listFailed),
    refetch: () => {
      if (!slug) void refetchList();
      else void refetch();
    },
  };
}
