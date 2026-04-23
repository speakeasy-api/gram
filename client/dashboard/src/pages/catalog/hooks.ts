import { useSdkClient } from "@/contexts/Sdk";
import { ExternalMCPServer } from "@gram/client/models/components";
import { queryKeyListMCPCatalog } from "@gram/client/react-query";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";

interface ServerMeta {
  comPulsemcpServer?: {
    visitorsEstimateMostRecentWeek?: number;
    visitorsEstimateLastFourWeeks?: number;
    visitorsEstimateTotal?: number;
    isOfficial?: boolean;
  };
  comPulsemcpServerVersion?: {
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

export function useInfiniteListMCPCatalog(
  search?: string,
  registryId?: string,
) {
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
