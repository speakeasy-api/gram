import { defineFilters, type OptionsById } from "@/components/filters";
import type { FilterValues } from "@/components/filters";
import {
  type AuthType,
  type FilterValues as CatalogFilterValues,
  type PopularityThreshold,
  type ToolBehavior,
  type ToolCountThreshold,
  type UpdatedRange,
} from "./filter-defaults";

/**
 * Catalog filters expressed in the unified filter system.
 *
 * The dimension `id`s are deliberately the catalog's existing URL params
 * (`auth`, `behavior`, `minUsers`, `updated`, `minTools`), so bookmarked /
 * shared catalog URLs keep working after the migration. `category` and `sort`
 * are NOT filters — they stay page state and are handled separately.
 *
 * Threshold dimensions are modeled as single-select: the "0 / any" option is
 * the absent (null) value, matching the old behavior where the param was
 * deleted at the default. The values are strings (URL-native); {@link
 * toCatalogFilterValues} converts them back to the numeric/enum shape that
 * `filterAndSortServers` already consumes.
 */
export const CATALOG_FILTERS = defineFilters([
  { id: "auth", label: "Auth type", kind: "multiselect" },
  { id: "behavior", label: "Tool behavior", kind: "multiselect" },
  { id: "minUsers", label: "Popularity", kind: "select", allLabel: "All" },
  { id: "updated", label: "Last updated", kind: "select", allLabel: "All" },
  { id: "minTools", label: "Tool count", kind: "select", allLabel: "All" },
]);

/** Option lists for the catalog's select/multiselect dimensions. */
export const CATALOG_FILTER_OPTIONS: OptionsById = {
  auth: [
    { value: "none", label: "No Auth" },
    { value: "apikey", label: "API Key" },
    { value: "oauth", label: "OAuth 2.1" },
    { value: "other", label: "Other" },
  ],
  behavior: [
    { value: "readonly", label: "Read-only only" },
    { value: "write", label: "Can modify data" },
  ],
  minUsers: [
    { value: "100", label: "100+ users" },
    { value: "1000", label: "1k+ users" },
    { value: "10000", label: "10k+ users" },
  ],
  updated: [
    { value: "week", label: "This week" },
    { value: "month", label: "This month" },
    { value: "year", label: "This year" },
  ],
  minTools: [
    { value: "5", label: "5+ tools" },
    { value: "10", label: "10+ tools" },
  ],
};

// Allowed values per dimension. The unified filter state reads raw URL params,
// so we sanitize to these before casting — an arbitrary `?auth=bogus` would
// otherwise filter out every result.
const VALID_AUTH_TYPES: readonly AuthType[] = [
  "none",
  "apikey",
  "oauth",
  "other",
];
const VALID_BEHAVIORS: readonly ToolBehavior[] = ["readonly", "write"];
const VALID_MIN_USERS: readonly PopularityThreshold[] = [100, 1000, 10000];
const VALID_UPDATED: readonly UpdatedRange[] = ["week", "month", "year"];
const VALID_MIN_TOOLS: readonly ToolCountThreshold[] = [5, 10];

/**
 * Bridge the unified value object back to the catalog's `FilterValues` shape so
 * the existing `filterAndSortServers` logic is reused verbatim. Values are
 * sanitized to known options (invalid/absent collapse to the "any" sentinel:
 * 0 / "any").
 */
export function toCatalogFilterValues(
  values: FilterValues<typeof CATALOG_FILTERS>,
): CatalogFilterValues {
  const minUsers = Number(values.minUsers);
  const minTools = Number(values.minTools);
  return {
    authTypes: values.auth.filter((v): v is AuthType =>
      VALID_AUTH_TYPES.includes(v as AuthType),
    ),
    toolBehaviors: values.behavior.filter((v): v is ToolBehavior =>
      VALID_BEHAVIORS.includes(v as ToolBehavior),
    ),
    minUsers: VALID_MIN_USERS.includes(minUsers as PopularityThreshold)
      ? (minUsers as PopularityThreshold)
      : 0,
    updatedRange: VALID_UPDATED.includes(values.updated as UpdatedRange)
      ? (values.updated as UpdatedRange)
      : "any",
    minTools: VALID_MIN_TOOLS.includes(minTools as ToolCountThreshold)
      ? (minTools as ToolCountThreshold)
      : 0,
  };
}

/** Whether any granular filter is active (drives chips + empty-state copy). */
export function hasActiveCatalogFilters(filters: CatalogFilterValues): boolean {
  return (
    filters.authTypes.length > 0 ||
    filters.toolBehaviors.length > 0 ||
    filters.minUsers > 0 ||
    filters.updatedRange !== "any" ||
    filters.minTools > 0
  );
}
