import type { OrganizationRemoteSessionIssuer } from "@gram/client/models/components/organizationremotesessionissuer.js";
import { useOrganizationRemoteSessionIssuersInfinite } from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import { useEffect, useMemo } from "react";

export type OrganizationIssuerTier = {
  items: OrganizationRemoteSessionIssuer[];
  /** Still loading the tier: its first page, or with `drain`, any page. */
  isLoading: boolean;
  isError: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  loadMore: () => void;
};

// One tier of the organization's issuers, listed on its own so the platform
// catalog cannot fill the pages meant for the organization's providers.
//
// `drain` walks every page, for tiers bounded by what the organization itself
// created (organizational and project-specific), whose full list other views
// on the page need, such as the consolidate picker. The platform tier is not
// drained: it grows with the shared catalog, so it pages on request instead.
export function useOrganizationIssuerTier(
  tier: "organization" | "project" | "platform",
  { drain }: { drain: boolean },
): OrganizationIssuerTier {
  const query = useOrganizationRemoteSessionIssuersInfinite(
    { tier },
    undefined,
    { throwOnError: false },
  );
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = query;

  useEffect(() => {
    if (drain && hasNextPage && !isFetchingNextPage) void fetchNextPage();
  }, [drain, hasNextPage, isFetchingNextPage, fetchNextPage]);

  const items = useMemo(
    () => query.data?.pages.flatMap((page) => page.result.items) ?? [],
    [query.data],
  );

  return {
    items,
    // A drained tier is not done until its last page lands: deciding from a
    // partial list is exactly what this hook exists to prevent.
    isLoading: query.isLoading || (drain && !!hasNextPage),
    isError: query.isError,
    hasMore: !drain && !!hasNextPage,
    loadingMore: isFetchingNextPage,
    loadMore: () => void fetchNextPage(),
  };
}
