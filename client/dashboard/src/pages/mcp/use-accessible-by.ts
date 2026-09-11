import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildIdentityAccessQuery } from "@gram/client/react-query/identityAccess.js";

/** One selected person's answer: their reachable server ids, or null if unsettled. */
type Answer = string[] | null;

/**
 * Every mcp_servers id reachable by the selected people.
 *
 * access.listIdentityAccess answers for one person at a time, so selecting
 * several asks it several times and unions the answers — a server matches the
 * filter if *anyone* selected can reach it. The reads are ordinary cached
 * queries keyed by user, so re-picking someone costs nothing.
 *
 * The set is `undefined` until every selected person's read has landed.
 * Returning a partial union would quietly hide servers the missing person can
 * reach, which reads as an answer rather than as a wait.
 */
export function useAccessibleServerIds(userIds: string[]): {
  serverIds: ReadonlySet<string> | undefined;
  isLoading: boolean;
  isError: boolean;
} {
  const client = useGramContext();

  const queries = useQueries({
    queries: userIds.map((userId) => ({
      ...buildIdentityAccessQuery(client, { userId }),
      throwOnError: false,
    })),
  });

  const isLoading = queries.some((query) => query.isLoading);
  const isError = queries.some((query) => query.isError);

  // useQueries hands back a fresh array every render, so the set is memoized
  // against the settled rows rather than against that array's identity.
  const answers = JSON.stringify(
    queries.map<Answer>((query) =>
      query.data ? query.data.servers.map((server) => server.id) : null,
    ),
  );

  const serverIds = useMemo(() => {
    const parsed = JSON.parse(answers) as Answer[];
    if (parsed.length === 0) return undefined;
    if (parsed.some((answer) => answer === null)) return undefined;
    const set = new Set<string>();
    for (const answer of parsed) {
      for (const id of answer ?? []) set.add(id);
    }
    return set;
  }, [answers]);

  return { serverIds, isLoading, isError };
}
