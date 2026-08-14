import { useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";

import {
  filtersToSearch,
  type FilterSelection,
} from "@/lib/organizationFilters";
import type { OrganizationsSearch } from "@/routes/organizations.index";

const ROUTE_ID = "/organizations/";

/**
 * Writes a chosen set to the URL. All three params come from the set, so a
 * control that names one filter clears the two it does not.
 */
export function useApplyFilters(): (next: FilterSelection) => void {
  const navigate = useNavigate({ from: ROUTE_ID });

  return useCallback(
    (next: FilterSelection): void => {
      void navigate({
        search: (prev: OrganizationsSearch) => ({
          ...prev,
          ...filtersToSearch(next),
          // Page 1. The rows a page-two cursor points at were counted under the
          // filters that minted it.
          page: undefined,
        }),
      });
    },
    [navigate],
  );
}
