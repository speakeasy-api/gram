import { useSdkClient } from "@/contexts/Sdk";
import { ExternalMCPServer } from "@gram/client/models/components";
import { useListMCPRegistries } from "@gram/client/react-query";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

interface RegistryRemoteMeta {
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
}

export interface ServerMetaServer {
  visitorsEstimateMostRecentWeek?: number;
  visitorsEstimateLastFourWeeks?: number;
  visitorsEstimateTotal?: number;
  isOfficial?: boolean;
}

export interface ServerMetaVersion {
  source?: string;
  status?: string;
  publishedAt?: string;
  updatedAt?: string;
  isLatest?: boolean;
  "remotes[0]"?: RegistryRemoteMeta;
}

interface ServerMeta {
  "com.pulsemcp/server"?: ServerMetaServer;
  "com.pulsemcp/server-version"?: ServerMetaVersion;
  "ai.getgram/server"?: ServerMetaServer;
  "ai.getgram/server-version"?: ServerMetaVersion;
}

export type Server = ExternalMCPServer & {
  meta: ServerMeta;
};

type CursorsMap = Record<string, string | undefined>;

export function useInfiniteServeMCPRegistry(search?: string) {
  const client = useSdkClient();
  const [debouncedSearch, setDebouncedSearch] = useState(search);

  const { data: registriesData } = useListMCPRegistries();
  const registrySlugs = useMemo(
    () =>
      (registriesData?.registries ?? [])
        .map((r) => r.slug)
        .filter((s): s is string => s != null),
    [registriesData],
  );

  useEffect(() => {
    const handler = setTimeout(() => {
      setDebouncedSearch(search);
    }, 300);

    return () => {
      clearTimeout(handler);
    };
  }, [search]);

  const query = useInfiniteQuery({
    queryKey: ["serveMCPRegistry", registrySlugs, debouncedSearch],
    queryFn: async ({ pageParam }) => {
      const cursors: CursorsMap = pageParam ?? {};
      // On first page, serve all registries; on subsequent pages, only those with cursors
      const slugsToFetch =
        Object.keys(cursors).length === 0
          ? registrySlugs
          : registrySlugs.filter((slug) => cursors[slug] != null);

      const results = await Promise.all(
        slugsToFetch.map(async (slug) => {
          const result = await client.mcpRegistries.serve({
            registrySlug: slug,
            search: debouncedSearch || undefined,
            cursor: cursors[slug],
          });
          return { slug, ...result };
        }),
      );

      const servers = results.flatMap((r) => r.servers);
      const nextCursors: CursorsMap = {};
      let hasMore = false;
      for (const r of results) {
        if (r.nextCursor) {
          nextCursors[r.slug] = r.nextCursor;
          hasMore = true;
        }
      }

      return { servers, nextCursors: hasMore ? nextCursors : null };
    },
    initialPageParam: undefined as CursorsMap | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursors ?? undefined,
    enabled: registrySlugs.length > 0,
    staleTime: 5 * 60 * 1000,
  });

  return { ...query, debouncedSearch };
}
