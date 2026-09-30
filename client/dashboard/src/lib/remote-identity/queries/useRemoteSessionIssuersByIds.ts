import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildRemoteSessionIssuerQuery } from "@gram/client/react-query/remoteSessionIssuer.js";
import { type UseQueryResult, useQueries } from "@tanstack/react-query";

type IssuersByIds = {
  items: RemoteSessionIssuer[];
  isLoading: boolean;
  isError: boolean;
};

// Module scope so the reference is stable: useQueries re-runs an inline
// combine on every render, handing consumers a fresh items array each time.
function combineIssuers(
  results: UseQueryResult<RemoteSessionIssuer>[],
): IssuersByIds {
  return {
    items: results.flatMap((result) => (result.data ? [result.data] : [])),
    isLoading: results.some((result) => result.isLoading),
    isError: results.some((result) => result.isError),
  };
}

// Resolve a known set of remote_session_issuers by id. Whether a provider is
// attached must never be answered by filtering the paginated listing: that
// listing spans the project, its organization and the whole platform catalog,
// so an attached issuer can sit past any page. The attached set is small
// (typically one or two), so one get per id costs little however large the
// catalog grows. Shares cache entries with useRemoteSessionIssuer, so
// invalidateAllRemoteSessionIssuer refreshes both.
export function useRemoteSessionIssuersByIds(
  ids: readonly string[],
  options?: { enabled?: boolean },
): IssuersByIds {
  const client = useGramContext();
  const enabled = options?.enabled ?? true;

  return useQueries({
    queries: ids.map((id) => ({
      ...buildRemoteSessionIssuerQuery(client, { id }),
      enabled,
      throwOnError: false,
    })),
    combine: combineIssuers,
  });
}
