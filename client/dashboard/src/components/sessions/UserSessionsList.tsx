import type { QueryParamStatus as ListUserSessionsQueryParamStatus } from "@gram/client/models/operations/listusersessions.js";
import { useUserSessionsInfinite } from "@gram/client/react-query/userSessions.js";

import { SessionRow } from "./SessionRow";
import { ListStateBoundary, LoadMoreRow } from "./ListStateBoundary";

/**
 * Chrome-free list of an issuer's active sessions. Mounted on the Clients and
 * Sessions tab of both MCP server detail page families.
 *
 * When clientId is set the list is scoped to a single registered client, which
 * is how the clients listing below it drills in.
 */
export function UserSessionsList({
  issuerId,
  clientId,
}: {
  issuerId: string;
  clientId?: string;
}): JSX.Element {
  const {
    data,
    isPending,
    isError,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    refetch,
  } = useUserSessionsInfinite({
    userSessionIssuerId: issuerId,
    clientId,
    status: "active" as ListUserSessionsQueryParamStatus,
  });
  const sessions = data?.pages.flatMap((p) => p.result.items) ?? [];

  const emptyMessage = clientId
    ? "No active sessions for this client"
    : "No active sessions";

  return (
    <>
      <ListStateBoundary
        isPending={isPending}
        // Only an initial-load failure replaces the list. A failed later
        // page keeps the rows already on screen, with the Load more button
        // left to retry.
        isError={isError && sessions.length === 0}
        isEmpty={sessions.length === 0}
        errorMessage="Couldn't load sessions."
        emptyMessage={emptyMessage}
        onRetry={() => void refetch()}
      >
        <ul className="divide-border divide-y border">
          {sessions.map((s) => (
            <SessionRow
              key={s.id}
              session={s}
              onRevoked={() => void refetch()}
            />
          ))}
        </ul>
      </ListStateBoundary>
      <LoadMoreRow
        hasNextPage={hasNextPage}
        isFetchingNextPage={isFetchingNextPage}
        onLoadMore={() => void fetchNextPage()}
      />
    </>
  );
}
