import { useSession } from "@/contexts/Auth";
import { useAllRemoteSessionClients } from "@/pages/mcp/x/tabs/settings/sections/authentication/useAllRemoteSessionClients";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useRemoteSessionsInfinite } from "@gram/client/react-query/remoteSessions.js";
import { useEffect, useMemo } from "react";

/**
 * Mirror of the gateway's all-or-nothing rule (remotesessions.ResolveAccessTokens):
 * connected iff every client bound to the issuer has a session, matched on
 * client id alone because the gateway's lookup key is (subject, client). Zero
 * bound clients means nothing to link. Token validity is deliberately ignored —
 * stale credentials 401 at the gateway and surface the Connect prompt there.
 */
export function computeHasUserConnected(
  boundClients: ReadonlyArray<{ id: string }>,
  subjectSessions: ReadonlyArray<Pick<RemoteSession, "remoteSessionClientId">>,
): boolean {
  const linkedClientIds = new Set(
    subjectSessions.map((s) => s.remoteSessionClientId),
  );
  return boundClients.every((c) => linkedClientIds.has(c.id));
}

/**
 * Whether the current user holds the upstream remote_sessions an issuer-gated
 * remote MCP server requires. Read-only: never mints a user-session JWT.
 *
 * `undefined` while resolving. On lookup errors: a 403 resolves to `true` —
 * the lists need project:read, which a viewer holding only mcp:read may lack,
 * and an unanswerable pre-check must not block the tools listing; any other
 * error resolves to `false`, because minting is the one action this hook
 * exists to prevent and a backend blip must not trigger it.
 *
 * Known limit: the session list is project-scoped, so a link made under
 * another project's issuer on a shared org-level client reads as disconnected.
 * That fails to the Connect prompt and heals on reconnect.
 */
export function useHasUserConnected({
  userSessionIssuerId,
}: {
  userSessionIssuerId: string;
}): boolean | undefined {
  const session = useSession();

  const clientsQuery = useAllRemoteSessionClients(
    { userSessionIssuerId },
    { throwOnError: false },
  );

  const sessionsQuery = useRemoteSessionsInfinite(
    { subjectUrn: `user:${session.user.id}` },
    undefined,
    { throwOnError: false },
  );

  const { hasNextPage, isFetchingNextPage, fetchNextPage } = sessionsQuery;
  const subjectSessions = useMemo<RemoteSession[]>(
    () => sessionsQuery.data?.pages.flatMap((page) => page.result.items) ?? [],
    [sessionsQuery.data],
  );

  const failure = clientsQuery.error ?? sessionsQuery.error;

  let hasUserConnected: boolean | undefined;
  if (failure) {
    hasUserConnected = isForbidden(failure);
  } else if (clientsQuery.isLoading) {
    hasUserConnected = undefined;
  } else if (computeHasUserConnected(clientsQuery.items, subjectSessions)) {
    // Final even mid-walk: further pages can only add sessions.
    hasUserConnected = true;
  } else if (sessionsQuery.isLoading || hasNextPage) {
    hasUserConnected = undefined;
  } else {
    hasUserConnected = false;
  }

  // Walk further session pages only while the verdict is still open — a
  // partial list must not be read as disconnected, but once every bound
  // client is matched there is nothing left to fetch.
  const undecided = hasUserConnected === undefined;
  useEffect(() => {
    if (undecided && hasNextPage && !isFetchingNextPage) {
      void fetchNextPage();
    }
  }, [undecided, hasNextPage, isFetchingNextPage, fetchNextPage]);

  return hasUserConnected;
}

function isForbidden(error: Error): boolean {
  return error instanceof GramError && error.statusCode === 403;
}
