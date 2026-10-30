import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";
import type { Category } from "../CategoryTabs";
import type { AuthType, FilterValues, UpdatedRange } from "../filter-defaults";

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
  category: Category;
  sort: SortOption;
  filters: FilterValues;
}

const VALID_SORT_OPTIONS: readonly SortOption[] = [
  "recent",
  "updated",
  "alphabetical",
  "alphabetical-desc",
];

const VALID_CATEGORIES: readonly Category[] = ["all"];

const VALID_AUTH_TYPES: readonly AuthType[] = [
  "none",
  "apikey",
  "oauth",
  "other",
  "unknown",
];
const VALID_UPDATED_RANGES: readonly UpdatedRange[] = [
  "any",
  "week",
  "month",
  "year",
];

/**
 * Hook to manage filter state with URL synchronization.
 *
 * Only native-evidence filters are supported; retired URL filters are ignored.
 */
export interface UseFilterStateReturn extends FilterState {
  setCategory: (category: Category) => void;
  setSort: (sort: SortOption) => void;
  setFilters: (filters: FilterValues) => void;
  clearFilters: () => void;
}

export function useFilterState(): UseFilterStateReturn {
  const [searchParams, setSearchParams] = useSearchParams();

  // Parse current filter state from URL
  const filterState = useMemo<FilterState>(() => {
    // Category
    const categoryParam = searchParams.get("category");
    const category: Category = VALID_CATEGORIES.includes(
      categoryParam as Category,
    )
      ? (categoryParam as Category)
      : "all";

    // Sort
    const sortParam = searchParams.get("sort");
    const sort: SortOption = VALID_SORT_OPTIONS.includes(
      sortParam as SortOption,
    )
      ? (sortParam as SortOption)
      : "alphabetical";

    // Auth types
    const authParam = searchParams.get("auth");
    const authTypes: AuthType[] = authParam
      ? (authParam
          .split(",")
          .filter((t) =>
            VALID_AUTH_TYPES.includes(t as AuthType),
          ) as AuthType[])
      : [];

    // Updated range
    const updatedParam = searchParams.get("updated");
    const updatedRange: UpdatedRange = VALID_UPDATED_RANGES.includes(
      updatedParam as UpdatedRange,
    )
      ? (updatedParam as UpdatedRange)
      : "any";

    return {
      category,
      sort,
      filters: {
        authTypes,
        updatedRange,
        // The granular filters (incl. setup) flow through the unified filter
        // system in Catalog.tsx; this legacy hook only supplies category/sort.
        setupTypes: [],
      },
    };
  }, [searchParams]);

  // Update URL with new filter state
  const updateSearchParams = useCallback(
    (updates: Partial<FilterState>) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);

          if (updates.category !== undefined) {
            if (updates.category === "all") {
              params.delete("category");
            } else {
              params.set("category", updates.category);
            }
          }

          if (updates.sort !== undefined) {
            if (updates.sort === "alphabetical") {
              params.delete("sort");
            } else {
              params.set("sort", updates.sort);
            }
          }

          if (updates.filters !== undefined) {
            const f = updates.filters;

            // Auth types
            if (f.authTypes.length > 0) {
              params.set("auth", f.authTypes.join(","));
            } else {
              params.delete("auth");
            }

            // Updated range
            if (f.updatedRange !== "any") {
              params.set("updated", f.updatedRange);
            } else {
              params.delete("updated");
            }
          }

          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setCategory = useCallback(
    (category: Category) => updateSearchParams({ category }),
    [updateSearchParams],
  );

  const setSort = useCallback(
    (sort: SortOption) => updateSearchParams({ sort }),
    [updateSearchParams],
  );

  const setFilters = useCallback(
    (filters: FilterValues) => updateSearchParams({ filters }),
    [updateSearchParams],
  );

  const clearFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.delete("category");
        params.delete("sort");
        // Keep in sync with CATALOG_FILTERS dimension ids — every granular
        // filter param must be cleared here or it survives "Clear filters".
        params.delete("auth");
        params.delete("setup");
        params.delete("behavior");
        params.delete("minUsers");
        params.delete("updated");
        params.delete("minTools");
        return params;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  return {
    ...filterState,
    setCategory,
    setSort,
    setFilters,
    clearFilters,
  };
}
