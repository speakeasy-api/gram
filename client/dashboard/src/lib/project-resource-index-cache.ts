import type { QueryClient } from "@tanstack/react-query";

export const PROJECT_RESOURCE_INDEX_QUERY_PREFIX = [
  "project-resource-index",
] as const;

/** Conservatively invalidate the complete index after any successful mutation. */
export function invalidateProjectResourceIndexes(
  queryClient: Pick<QueryClient, "invalidateQueries">,
): Promise<void> {
  return queryClient.invalidateQueries({
    queryKey: PROJECT_RESOURCE_INDEX_QUERY_PREFIX,
  });
}
