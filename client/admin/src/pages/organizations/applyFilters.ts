import { useNavigate, useSearch } from "@tanstack/react-router";
import { createContext, useCallback, useContext } from "react";

import {
  filtersToSearch,
  statusSelection,
  type FilterSelection,
} from "@/lib/organizationFilters";
import type { OrganizationsSearch } from "@/routes/organizations.index";

const ROUTE_ID = "/organizations/";

export type ApplyOptions = {
  /**
   * Drops the search term too. A stat figure counts rows no term narrowed, so
   * the list it opens cannot keep one. The sheet's own controls do not.
   */
  clearSearch?: boolean;
};

// What applying leaves for the page to do itself. Two things it cannot read
// off the URL: the page is component state, so applying the set already
// applied moves nothing it watches, and a search term the box has not
// committed yet is in no URL at all.
export const FiltersApplied = createContext<(options: ApplyOptions) => void>(
  () => {},
);

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
  const search = useSearch({ from: ROUTE_ID });

  return useCallback(
    (next: FilterSelection, options: ApplyOptions = {}): void => {
      const { createdPreset: nextPreset, ...nextFilters } =
        filtersToSearch(next);
      const { createdPreset: currentPreset, ...currentFilters } =
        filtersToSearch({
          ...search,
          type: search.type ?? [],
          trial: search.trial ?? [],
          disabled: statusSelection(search),
        });
      // A preset label is not a request filter. Only that metadata changing
      // preserves the page; an unchanged Apply still intentionally resets it.
      const metadataOnly =
        nextPreset !== currentPreset &&
        JSON.stringify(nextFilters) === JSON.stringify(currentFilters);
      if (!metadataOnly || options.clearSearch) onApplied(options);
      void navigate({
        search: (prev: OrganizationsSearch) => ({
          ...prev,
          ...filtersToSearch(next),
          ...(options.clearSearch ? { q: undefined } : {}),
        }),
      });
    },
    [navigate, onApplied, search],
  );
}
