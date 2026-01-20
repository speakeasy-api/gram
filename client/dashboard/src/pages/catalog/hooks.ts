import { useSdkClient } from "@/contexts/Sdk";
import { ExternalMCPServer } from "@gram/client/models/components";
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
      auth?: {
        type?: string;
      };
    };
  };
}

export type Server = ExternalMCPServer & {
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

  return useInfiniteQuery({
    queryKey: queryKeyListMCPCatalog({ search: debouncedSearch, registryId }),
    queryFn: async ({ pageParam }) => {
      return client.mcpRegistries.listCatalog({
        search: debouncedSearch || undefined,
        registryId: registryId || undefined,
        cursor: pageParam,
      });
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  });
}
