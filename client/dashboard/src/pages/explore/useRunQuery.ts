import { exploreDimensionValues } from "@gram/client/funcs/exploreDimensionValues.js";
import { exploreQuery } from "@gram/client/funcs/exploreQuery.js";
import type { ExploreQueryResult } from "@gram/client/models/components/explorequeryresult.js";
import type { QueryRequestBody } from "@gram/client/models/components/queryrequestbody.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { windowRange, type WindowPreset } from "./exploreModel";

/**
 * Runs one explore query. The generated useExploreQuery hook keys its cache
 * on the session only — it ignores the request body — so this drives
 * useQuery directly with a key that encodes the whole spec. Both the builder
 * and every dashboard widget share the "explore-query" prefix, so they can
 * be invalidated together. Windows resolve hour-aligned, so keys are stable
 * within the hour.
 */
export function useRunQuery(
  body: QueryRequestBody,
  enabled: boolean,
): UseQueryResult<ExploreQueryResult, Error> {
  const client = useGramContext();
  return useQuery({
    queryKey: [
      "explore-query",
      { ...body, from: body.from.toISOString(), to: body.to.toISOString() },
    ],
    enabled,
    retry: false,
    queryFn: () =>
      unwrapAsync(exploreQuery(client, { queryRequestBody: body })),
  });
}

/**
 * A dimension's most frequent values in the dataset inside the builder's
 * window — what populates the filter value pickers. Keyed on the resolved
 * hour-aligned range so suggestions follow the window without refetching
 * every render.
 */
export function useDimensionValues(
  dataset: string,
  dimension: string,
  window: WindowPreset,
): UseQueryResult<string[], Error> {
  const client = useGramContext();
  const { from, to } = windowRange(window);
  return useQuery({
    queryKey: [
      "explore-dimension-values",
      dataset,
      dimension,
      from.toISOString(),
      to.toISOString(),
    ],
    enabled: dimension !== "",
    retry: false,
    staleTime: 60_000,
    queryFn: async () => {
      const res = await unwrapAsync(
        exploreDimensionValues(client, { dataset, dimension, from, to }),
      );
      return res.values;
    },
  });
}

/** The customer-facing message of a failed explore query, or null. */
export function resultErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof Error) return error.message;
  return "Query failed";
}
