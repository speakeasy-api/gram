import { useRemoteSessions } from "@gram/client/react-query/remoteSessions.js";

/**
 * Whether anyone has actually signed in through a client.
 *
 * Only the empty answer is worth surfacing — a client people already use needs
 * no comment — so this asks for a single row rather than a count, and returns
 * null while it does not know. A pending query must never render as "nobody
 * has connected".
 */
export function useClientHasSessions(
  remoteSessionClientId: string | undefined,
  options?: { enabled?: boolean },
): boolean | null {
  const enabled = (options?.enabled ?? true) && !!remoteSessionClientId;

  const query = useRemoteSessions(
    { remoteSessionClientId: remoteSessionClientId ?? "", limit: 1 },
    undefined,
    { enabled },
  );

  if (!enabled || !query.data) return null;
  return query.data.result.items.length > 0;
}
