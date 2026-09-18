import { useProject } from "@/contexts/Auth";
import { analyticsQuery } from "@gram/client/funcs/analyticsQuery.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import type { AnalyticsQueryResult } from "@gram/client/models/components/analyticsqueryresult.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import {
  keepPreviousData,
  useQuery,
  type UseQueryResult,
} from "@tanstack/react-query";

const EXPLORE_QUERY_KEY = "explore-query";

/** The query as it keys the cache: the body with its dates as text. */
function queryKeyBody(body: AnalyticsQueryPayload): unknown {
  return { ...body, from: body.from.toISOString(), to: body.to.toISOString() };
}

/**
 * Runs one analytics query for the builder, or nothing when there is no
 * query to run. Every change to the body supersedes the request in flight:
 * the key changes, the old request loses its observer, and the abort signal
 * it was given fires, so cost follows questions asked rather than characters
 * typed. The last result stays on screen while the next one loads, so a
 * refinement never blanks the page; `isPlaceholderData` tells the two apart.
 *
 * The generated hook keys its cache on the session alone and ignores the
 * body, which is why this drives useQuery directly. Windows resolve
 * hour-aligned, so keys are stable within the hour. The project joins the key
 * because the same body asked in two projects is two different questions, and
 * a shared key would serve one project's rows to the other until they went
 * stale.
 */
export function useRunQuery(
  body: AnalyticsQueryPayload | null,
): UseQueryResult<AnalyticsQueryResult, Error> {
  const client = useGramContext();
  const project = useProject();
  return useQuery({
    queryKey: [
      EXPLORE_QUERY_KEY,
      project.id,
      body === null ? null : queryKeyBody(body),
    ],
    enabled: body !== null,
    retry: false,
    staleTime: 60_000,
    placeholderData: keepPreviousData,
    queryFn: ({ signal }) =>
      unwrapAsync(
        analyticsQuery(
          client,
          {
            analyticsQueryPayload: body ?? {
              dataset: "",
              from: new Date(0),
              to: new Date(0),
            },
          },
          undefined,
          { fetchOptions: { signal } },
        ),
      ),
  });
}
