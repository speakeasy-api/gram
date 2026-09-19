import { useProject } from "@/contexts/Auth";
import { analyticsQuery } from "@gram/client/funcs/analyticsQuery.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import type { AnalyticsQueryResult } from "@gram/client/models/components/analyticsqueryresult.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

const EXPLORE_QUERY_KEY = "explore-query";

/** The query as it keys the cache: the body with its dates as text. */
function queryKeyBody(body: AnalyticsQueryPayload): unknown {
  return { ...body, from: body.from.toISOString(), to: body.to.toISOString() };
}

/**
 * Runs one analytics query for the builder, or nothing when there is no
 * query to run. A new body supersedes the request in flight: the key
 * changes, the old request loses its observer, and the abort signal it was
 * given fires, so a run pressed twice in quick succession costs one answer.
 * The panel shows the new run loading rather than the old rows, so what is
 * on screen always belongs to the query named beside it.
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
