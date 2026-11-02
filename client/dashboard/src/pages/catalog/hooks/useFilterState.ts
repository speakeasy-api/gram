import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";
import { CATALOG_FILTERS } from "../catalog-filter-schema";
import type { FilterValues } from "../filter-defaults";

/**
 * Sort options for the catalog listing.
 */
export type SortOption =
  | "recent"
  | "updated"
  | "alphabetical"
  | "alphabetical-desc";

/**
 * Complete filter state for the catalog.
 */
export interface FilterState {
  sort: SortOption;
  filters: FilterValues;
}

const VALID_SORT_OPTIONS: readonly SortOption[] = [
  "recent",
  "updated",
  "alphabetical",
  "alphabetical-desc",
];

/**
 * Hook to manage the catalog sort with URL synchronization. Granular filters
 * run through the unified filter system (`CATALOG_FILTERS`).
 */
export interface UseFilterStateReturn {
  sort: SortOption;
  setSort: (sort: SortOption) => void;
  clearFilters: () => void;
}

export function useFilterState(): UseFilterStateReturn {
  const [searchParams, setSearchParams] = useSearchParams();

  const sort = useMemo<SortOption>(() => {
    const sortParam = searchParams.get("sort");
    return VALID_SORT_OPTIONS.includes(sortParam as SortOption)
      ? (sortParam as SortOption)
      : "alphabetical";
  }, [searchParams]);

  const setSort = useCallback(
    (sort: SortOption) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          if (sort === "alphabetical") {
            params.delete("sort");
          } else {
            params.set("sort", sort);
          }
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const clearFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.delete("sort");
        for (const { id } of CATALOG_FILTERS) params.delete(id);
        return params;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  return { sort, setSort, clearFilters };
}
