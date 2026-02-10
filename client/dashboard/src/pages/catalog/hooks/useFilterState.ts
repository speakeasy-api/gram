import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";
import type { Category } from "../CategoryTabs";
import type {
  AuthType,
  FilterValues,
  PopularityThreshold,
  ToolBehavior,
  ToolCountThreshold,
  UpdatedRange,
} from "../FilterSidebar";

/**
 * Sort options for the catalog listing.
 */
export type SortOption =
  | "popular"
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

const defaultFilterValues: FilterValues = {
  authTypes: [],
  toolBehaviors: [],
  minUsers: 0,
  updatedRange: "any",
  minTools: 0,
};

const defaultFilterState: FilterState = {
  category: "all",
  sort: "popular",
  filters: defaultFilterValues,
};

const VALID_SORT_OPTIONS: readonly SortOption[] = [
  "popular",
  "recent",
  "updated",
  "alphabetical",
  "alphabetical-desc",
];

const VALID_CATEGORIES: readonly Category[] = ["all", "popular"];

const VALID_AUTH_TYPES: readonly AuthType[] = [
  "none",
  "apikey",
  "oauth",
  "other",
];
const VALID_BEHAVIORS: readonly ToolBehavior[] = ["readonly", "write"];
const VALID_UPDATED_RANGES: readonly UpdatedRange[] = [
  "any",
  "week",
  "month",
  "year",
];

/**
 * Hook to manage filter state with URL synchronization.
 *
 * URL param schema:
 * - category: "all" | "popular" | "official" | "safe" | "no-auth"
 * - sort: "popular" | "recent" | "updated" | "alphabetical" | "alphabetical-desc"
 * - auth: comma-separated auth types
 * - behavior: comma-separated behaviors
 * - minUsers: number
 * - updated: "any" | "week" | "month" | "year"
 * - minTools: number
 */
export function useFilterState() {
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
      : "popular";

    // Auth types
    const authParam = searchParams.get("auth");
    const authTypes: AuthType[] = authParam
      ? (authParam
          .split(",")
          .filter((t) =>
            VALID_AUTH_TYPES.includes(t as AuthType),
          ) as AuthType[])
      : [];

    // Tool behaviors
    const behaviorParam = searchParams.get("behavior");
    const toolBehaviors: ToolBehavior[] = behaviorParam
      ? (behaviorParam
          .split(",")
          .filter((b) =>
            VALID_BEHAVIORS.includes(b as ToolBehavior),
          ) as ToolBehavior[])
      : [];

    // Popularity
    const minUsersParam = searchParams.get("minUsers");
    const minUsers: PopularityThreshold =
      minUsersParam && [0, 100, 1000, 10000].includes(Number(minUsersParam))
        ? (Number(minUsersParam) as PopularityThreshold)
        : 0;

    // Updated range
    const updatedParam = searchParams.get("updated");
    const updatedRange: UpdatedRange = VALID_UPDATED_RANGES.includes(
      updatedParam as UpdatedRange,
    )
      ? (updatedParam as UpdatedRange)
      : "any";

    // Tool count
    const minToolsParam = searchParams.get("minTools");
    const minTools: ToolCountThreshold =
      minToolsParam && [0, 5, 10].includes(Number(minToolsParam))
        ? (Number(minToolsParam) as ToolCountThreshold)
        : 0;

    return {
      category,
      sort,
      filters: {
        authTypes,
        toolBehaviors,
        minUsers,
        updatedRange,
        minTools,
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
            if (updates.sort === "popular") {
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

            // Behaviors
            if (f.toolBehaviors.length > 0) {
              params.set("behavior", f.toolBehaviors.join(","));
            } else {
              params.delete("behavior");
            }

            // Popularity
            if (f.minUsers > 0) {
              params.set("minUsers", String(f.minUsers));
            } else {
              params.delete("minUsers");
            }

            // Updated range
            if (f.updatedRange !== "any") {
              params.set("updated", f.updatedRange);
            } else {
              params.delete("updated");
            }

            // Tool count
            if (f.minTools > 0) {
              params.set("minTools", String(f.minTools));
            } else {
              params.delete("minTools");
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
    setSearchParams({}, { replace: true });
  }, [setSearchParams]);

  return {
    ...filterState,
    setCategory,
    setSort,
    setFilters,
    clearFilters,
  };
}

export { defaultFilterState, defaultFilterValues, VALID_SORT_OPTIONS };
