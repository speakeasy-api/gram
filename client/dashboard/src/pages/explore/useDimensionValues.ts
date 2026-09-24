import { useProject } from "@/contexts/Auth";
import { analyticsDimensionValues } from "@gram/client/funcs/analyticsDimensionValues.js";
import type { AnalyticsDimensionValuesResult } from "@gram/client/models/components/analyticsdimensionvaluesresult.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { windowRange, type WindowPreset } from "./exploreModel";

const DIMENSION_VALUES_KEY = "explore-dimension-values";

/** The most values the endpoint returns in one answer. */
export const DIMENSION_VALUES_LIMIT = 200;

/**
 * The values a dimension holds inside the builder's window, most frequent
 * first, for a filter's picker. Fetched only while the picker is open, and
 * keyed by project, dataset, dimension and window, so a picker reopened on
 * the same question is served from cache and a different dimension never
 * sees another's values.
 *
 * The generated hook keys its cache on the session alone and ignores the
 * body, which is why this drives useQuery directly, as the query runner does.
 */
export function useDimensionValues(
  dataset: string,
  dimension: string,
  window: WindowPreset,
  enabled: boolean,
): UseQueryResult<AnalyticsDimensionValuesResult, Error> {
  const client = useGramContext();
  const project = useProject();
  const { from, to } = windowRange(window);
  return useQuery({
    queryKey: [
      DIMENSION_VALUES_KEY,
      project.id,
      dataset,
      dimension,
      from.toISOString(),
      to.toISOString(),
    ],
    enabled: enabled && dataset !== "" && dimension !== "",
    retry: false,
    staleTime: 60_000,
    queryFn: ({ signal }) =>
      unwrapAsync(
        analyticsDimensionValues(
          client,
          {
            dimensionValuesRequestBody: {
              dataset,
              dimension,
              from,
              to,
              limit: DIMENSION_VALUES_LIMIT,
            },
          },
          undefined,
          { fetchOptions: { signal } },
        ),
      ),
  });
}
