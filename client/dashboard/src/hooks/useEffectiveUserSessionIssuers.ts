import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useUserSessionIssuersInfinite } from "@gram/client/react-query/userSessionIssuers.js";
import { useMemo } from "react";
import {
  PAGE_FETCH_LIMIT,
  useDrainedPages,
} from "@/components/sessions/useDrainedPages";

export function useEffectiveUserSessionIssuers({
  projectSlug,
  mcpResourceId,
}: { projectSlug?: string; mcpResourceId?: string } = {}): {
  issuers: UserSessionIssuer[];
  organizationIssuers: UserSessionIssuer[];
  isLoading: boolean;
  isError: boolean;
} {
  const query = useUserSessionIssuersInfinite({
    limit: PAGE_FETCH_LIMIT,
    gramProject: projectSlug,
    mcpResourceId,
  });
  const { fetchNextPage, hasNextPage, isFetchingNextPage } = query;
  const { isTruncated } = useDrainedPages({
    pageCount: query.data?.pages.length ?? 0,
    hasNextPage: !!hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError: query.isFetchNextPageError,
    fetchNextPage,
  });

  const issuers = useMemo(
    () =>
      [...(query.data?.pages.flatMap((page) => page.result.items) ?? [])].sort(
        (left, right) => {
          const leftOrganizationOwned = left.projectId === "";
          const rightOrganizationOwned = right.projectId === "";
          if (leftOrganizationOwned !== rightOrganizationOwned) {
            return leftOrganizationOwned ? -1 : 1;
          }
          return left.slug.localeCompare(right.slug);
        },
      ),
    [query.data],
  );
  const organizationIssuers = useMemo(
    () => issuers.filter((issuer) => issuer.projectId === ""),
    [issuers],
  );

  return {
    issuers,
    organizationIssuers,
    isLoading:
      !query.isError &&
      !isTruncated &&
      (query.isLoading || !!hasNextPage || isFetchingNextPage),
    isError: query.isError || isTruncated,
  };
}
