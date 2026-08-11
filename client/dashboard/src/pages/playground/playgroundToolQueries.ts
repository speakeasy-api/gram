import { queryKeyInstance } from "@gram/client/react-query/instance.js";
import { invalidateAllListTools } from "@gram/client/react-query/listTools.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import type { QueryClient } from "@tanstack/react-query";

export async function invalidatePlaygroundToolQueries(
  queryClient: QueryClient,
  toolsetSlug: string,
): Promise<void> {
  await Promise.all([
    invalidateAllListTools(queryClient),
    invalidateAllToolset(queryClient),
    queryClient.invalidateQueries({
      queryKey: queryKeyInstance({ toolsetSlug }),
    }),
  ]);
}
