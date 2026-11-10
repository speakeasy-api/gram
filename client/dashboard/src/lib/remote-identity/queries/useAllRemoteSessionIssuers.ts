import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useRemoteSessionIssuersInfinite } from "@gram/client/react-query/remoteSessionIssuers.js";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { useMemo } from "react";

// Walk every page of the remote_session_issuers the project can use and return
// the flattened list, for pickers that offer the whole set. The declarative
// twin of useAllRemoteSessionClients.
export function useAllRemoteSessionIssuers(options?: { enabled?: boolean }): {
  items: RemoteSessionIssuer[];
  isLoading: boolean;
} {
  const query = useRemoteSessionIssuersInfinite({}, undefined, {
    enabled: options?.enabled,
    throwOnError: false,
  });
  useDrainInfiniteQuery(query, options?.enabled);

  const items = useMemo<RemoteSessionIssuer[]>(
    () => query.data?.pages.flatMap((page) => page.result.items) ?? [],
    [query.data],
  );

  // Loading until the last page lands, so a picker never offers a partial
  // list as the whole one.
  return {
    items,
    isLoading: query.isLoading || (query.hasNextPage && !query.isError),
  };
}
