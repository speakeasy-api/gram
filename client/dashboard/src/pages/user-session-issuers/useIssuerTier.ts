import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useOrganizationUserSessionIssuersInfinite } from "@gram/client/react-query/organizationUserSessionIssuers.js";
import { PAGE_FETCH_LIMIT } from "@/components/sessions/useDrainedPages";
import { useEffect, useMemo } from "react";

export type IssuerTier = {
  items: UserSessionIssuer[];
  /** Still loading any page of the tier. */
  isLoading: boolean;
  /** A page failed: the first (nothing to show) or a later one (partial). */
  isError: boolean;
  /** Loads the tier again after a failure. */
  retry: () => void;
};

/**
 * Every authorization server in one tier of the organization, across all pages.
 * Both tiers are bounded by what the organization created, and the consolidate
 * picker needs every candidate, so the whole tier is loaded.
 */
export function useIssuerTier(tier: "organization" | "project"): IssuerTier {
  const query = useOrganizationUserSessionIssuersInfinite(
    { tier, limit: PAGE_FETCH_LIMIT },
    undefined,
    { throwOnError: false },
  );
  const { hasNextPage, isFetchNextPageError, fetchNextPage } = query;
  // A failed page stops the walk: the last good page still reports a next
  // one, so retrying on every settle would loop without end.
  const draining = !!hasNextPage && !isFetchNextPageError;

  // Wait out any fetch, not only a next page: paging during a refresh would
  // interrupt it and leave stale pages behind.
  const { isFetching } = query;
  useEffect(() => {
    if (draining && !isFetching) void fetchNextPage();
  }, [draining, isFetching, fetchNextPage]);

  const items = useMemo(
    () => query.data?.pages.flatMap((page) => page.result.items) ?? [],
    [query.data],
  );

  return {
    items,
    // A retry after a failure counts as loading, so its progress shows
    // instead of the failure it is replacing.
    isLoading:
      query.isLoading || draining || (query.isError && query.isFetching),
    isError: query.isError,
    retry: () => void query.refetch(),
  };
}

/**
 * Retries only the tiers that failed, so a healthy one is not reloaded with
 * them. Undefined when none failed.
 */
export function retryFailedTiers(
  tiers: IssuerTier[],
): (() => void) | undefined {
  const failed = tiers.filter((tier) => tier.isError);
  if (failed.length === 0) return undefined;
  return () => {
    for (const tier of failed) tier.retry();
  };
}
