import { useSdkClient } from "@/contexts/Sdk";
import { ExternalMCPServer } from "@gram/client/models/components";
import { queryKeyListMCPCatalog } from "@gram/client/react-query";
import { useQuery } from "@tanstack/react-query";

interface ServerMeta {
  "com.pulsemcp/server"?: {
    visitorsEstimateMostRecentWeek?: number;
    visitorsEstimateLastFourWeeks?: number;
    visitorsEstimateTotal?: number;
    isOfficial?: boolean;
  };
  "com.pulsemcp/server-version"?: {
    source?: string;
    status?: string;
    publishedAt?: string;
    updatedAt?: string;
    isLatest?: boolean;
    "remotes[0]"?: {
      tools?: Array<{
        name: string;
        description?: string;
        annotations?: {
          title?: string;
          readOnlyHint?: boolean;
          destructiveHint?: boolean;
        };
      }>;
      authOptions?: {
        type?: string;
      }[];
    };
  };
}

export type PulseMCPServer = Omit<ExternalMCPServer, "meta"> & {
  meta: ServerMeta;
};

// The catalog is small and the backend returns the full list in a single
// response, so a plain query over the whole catalog is enough — searching,
// sorting, and filtering all happen client-side. An optional `search` is still
// forwarded so callers that only need a specific server (e.g. the detail page)
// can narrow the response.
function useListMCPCatalogImpl(search?: string, registryId?: string) {
  const client = useSdkClient();

  return useQuery({
    queryKey: queryKeyListMCPCatalog({
      search: search || undefined,
      registryId: registryId || undefined,
    }),
    queryFn: async () =>
      client.mcpRegistries.listCatalog({
        search: search || undefined,
        registryId: registryId || undefined,
      }),
    staleTime: 5 * 60 * 1000, // 5 minutes - won't refetch if data is fresh
  });
}

export function useListMCPCatalog(
  search?: string,
  registryId?: string,
): ReturnType<typeof useListMCPCatalogImpl> {
  return useListMCPCatalogImpl(search, registryId);
}
