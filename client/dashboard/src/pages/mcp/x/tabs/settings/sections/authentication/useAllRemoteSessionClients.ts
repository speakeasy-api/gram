import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { ListRemoteSessionClientsRequest } from "@gram/client/models/operations/listremotesessionclients.js";
import { useRemoteSessionClientsInfinite } from "@gram/client/react-query/remoteSessionClients.js";
import { useEffect, useMemo } from "react";

// Walk every page of remote_session_clients matching the filter and return
// the flattened list. The Authentication tab derivations (associated
// providers, affectedMcpServers) need the full set across paginated
// responses — a single-page fetch silently undercounts once a project
// crosses the default page size of 50. The Delete dialog walks pages
// imperatively inside its submit handler; this hook is the declarative
// equivalent for components that need the list in their render.
//
// AGE-2554 will likely move this to a backend aggregate endpoint when
// multi-client support lands; the helper exists so the temporary client-
// side walk stays out of every consumer.
export function useAllRemoteSessionClients(
  filters: Pick<
    ListRemoteSessionClientsRequest,
    "userSessionIssuerId" | "remoteSessionIssuerId"
  >,
  options?: { enabled?: boolean },
): { items: RemoteSessionClient[]; isLoading: boolean } {
  const query = useRemoteSessionClientsInfinite(filters, undefined, {
    enabled: options?.enabled,
  });

  const { hasNextPage, isFetchingNextPage, fetchNextPage } = query;
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage) {
      void fetchNextPage();
    }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  const items = useMemo<RemoteSessionClient[]>(
    () => query.data?.pages.flatMap((page) => page.result.items) ?? [],
    [query.data],
  );

  // Keep isLoading true while any fetch is in flight or more pages remain. A
  // background refetch can otherwise expose cached pages as complete and let a
  // consumer make decisions from a stale partial list.
  const isLoading = query.isFetching || hasNextPage;

  return { items, isLoading };
}
