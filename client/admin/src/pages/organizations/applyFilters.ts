import { useNavigate } from "@tanstack/react-router";
import { createContext, useCallback, useContext } from "react";

import {
  filtersToSearch,
  type FilterSelection,
} from "@/lib/organizationFilters";
import type { OrganizationsSearch } from "@/routes/organizations.index";

const ROUTE_ID = "/organizations/";

// The cursor is component state rather than a search param, so applying the
// set that is already applied changes nothing the pager watches. It is told.
export const FiltersApplied = createContext<() => void>(() => {});

export type ApplyOptions = {
  /**
   * Drops the search term too. A stat figure counts rows no term narrowed, so
   * the list it opens cannot keep one. The sheet's own controls do not.
   */
  clearSearch?: boolean;
};

/**
 * Writes a chosen set to the URL. All three params come from the set, so a
 * control that names one filter clears the two it does not.
 */
export function useApplyFilters(): (
  next: FilterSelection,
  options?: ApplyOptions,
) => void {
  const navigate = useNavigate({ from: ROUTE_ID });
  const onApplied = useContext(FiltersApplied);

  return useCallback(
    (next: FilterSelection, options: ApplyOptions = {}): void => {
      // Page 1. The rows a page-two cursor points at were counted under the
      // filters that minted it.
      onApplied();
      void navigate({
        search: (prev: OrganizationsSearch) => ({
          ...prev,
          ...filtersToSearch(next),
          ...(options.clearSearch ? { q: undefined } : {}),
        }),
      });
    },
    [navigate, onApplied],
  );
}
