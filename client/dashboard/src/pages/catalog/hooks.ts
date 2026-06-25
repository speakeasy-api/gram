import { useSdkClient } from "@/contexts/Sdk";
import {
  ExternalMCPServerEntry,
  ExternalMCPTool,
} from "@gram/client/models/components";
import { queryKeyListMCPCatalog } from "@gram/client/react-query";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";

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
    // The catalog list response strips per-tool definitions from the meta blob
    // to stay small; only the lightweight auth info is kept. Full tools come
    // from getServerDetails (see `tools` below, populated by enrichment).
    "remotes[0]"?: {
      authOptions?: {
        type?: string;
      }[];
    };
  };
}

// The catalog list returns `ExternalMCPServerEntry` (no tools). `tools` is
// optional and populated client-side by enrichment (getServerDetails) for the
// add/release flow that needs per-tool URNs.
export type PulseMCPServer = Omit<ExternalMCPServerEntry, "meta"> & {
  meta: ServerMeta;
  tools?: ExternalMCPTool[];
};

function useInfiniteListMCPCatalogImpl(search?: string, registryId?: string) {
  const client = useSdkClient();
  const [debouncedSearch, setDebouncedSearch] = useState(search);

  useEffect(() => {
    const handler = setTimeout(() => {
      setDebouncedSearch(search);
    }, 300);

    return () => {
      clearTimeout(handler);
    };
  }, [search]);

  const query = useInfiniteQuery({
    queryKey: queryKeyListMCPCatalog({
      search: debouncedSearch,
      registryId,
    }),
    queryFn: async ({ pageParam }) => {
      return client.mcpRegistries.listCatalog({
        search: debouncedSearch || undefined,
        registryId: registryId || undefined,
        cursor: pageParam,
      });
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    staleTime: 5 * 60 * 1000, // 5 minutes - won't refetch if data is fresh
  });

  // Return both the query result and the debounced search value
  // so consumers can check if the query state matches their expected search
  return { ...query, debouncedSearch };
}

export function useInfiniteListMCPCatalog(
  search?: string,
  registryId?: string,
): ReturnType<typeof useInfiniteListMCPCatalogImpl> {
  return useInfiniteListMCPCatalogImpl(search, registryId);
}
