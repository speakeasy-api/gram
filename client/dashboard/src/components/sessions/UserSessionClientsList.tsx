import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import { useUserSessionClientsInfinite } from "@gram/client/react-query/userSessionClients.js";

import { DotTable } from "@/components/ui/DotTable";
import { ClientTableRow } from "./ClientTableRow";
import { ListStateBoundary, LoadMoreRow } from "./ListStateBoundary";

/**
 * Chrome-free list of the clients registered against an issuer, DCR-registered
 * and CIMD-resolved alike. Mounted on the Clients and Sessions tab of both MCP
 * server detail page families.
 */
export function UserSessionClientsList({
  issuerId,
  onViewSessions,
  onClientRevoked,
}: {
  issuerId: string;
  onViewSessions: (client: UserSessionClient) => void;
  /**
   * Called after a revoke lands, so the owner can refresh the sessions the
   * revoke cascaded to. Revoking a client soft-deletes every session it
   * issued, which this list cannot refetch on its own.
   */
  onClientRevoked: (client: UserSessionClient) => void;
}): JSX.Element {
  const {
    data,
    isPending,
    isError,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    refetch,
  } = useUserSessionClientsInfinite({ userSessionIssuerId: issuerId });
  const clients = data?.pages.flatMap((p) => p.result.items) ?? [];

  return (
    <>
      <ListStateBoundary
        isPending={isPending}
        // Only an initial-load failure replaces the list. A failed later
        // page keeps the rows already on screen, with the Load more button
        // left to retry.
        isError={isError && clients.length === 0}
        isEmpty={clients.length === 0}
        errorMessage="Couldn't load clients."
        emptyMessage="No clients have registered with this server yet"
        onRetry={() => void refetch()}
      >
        <DotTable
          headers={[
            { label: "Client" },
            { label: "Source" },
            { label: "Registered" },
            { label: "", className: "w-10" },
          ]}
        >
          {clients.map((client) => (
            <ClientTableRow
              key={client.id}
              client={client}
              onRevoked={() => {
                void refetch();
                onClientRevoked(client);
              }}
              onViewSessions={onViewSessions}
            />
          ))}
        </DotTable>
      </ListStateBoundary>
      <LoadMoreRow
        hasNextPage={hasNextPage}
        isFetchingNextPage={isFetchingNextPage}
        onLoadMore={() => void fetchNextPage()}
      />
    </>
  );
}
