import { defineFilters, type OptionsById } from "@/components/filters";
import type { FilterValues } from "@/components/filters";
import {
  type AuthType,
  type FilterValues as CatalogFilterValues,
  type SetupType,
  type UpdatedRange,
} from "./filter-defaults";

/** Filters backed by native catalog evidence. */
export const CATALOG_FILTERS = defineFilters([
  { id: "auth", label: "Auth type", kind: "multiselect" },
  {
    id: "setup",
    label: "Setup",
    kind: "multiselect",
    description:
      "Whether setup is automatic, requires credentials, or is not yet confirmed.",
  },
  { id: "updated", label: "Last updated", kind: "select", allLabel: "All" },
]);

/** Option lists for the catalog's select/multiselect dimensions. */
export const CATALOG_FILTER_OPTIONS: OptionsById = {
  auth: [
    { value: "none", label: "No Auth" },
    { value: "apikey", label: "API Key" },
    { value: "oauth", label: "OAuth" },
    { value: "other", label: "Other" },
    { value: "unknown", label: "Unknown" },
  ],
  setup: [
    { value: "auto", label: "Automatic" },
    { value: "manual", label: "Manual setup" },
    { value: "unknown", label: "Setup unknown" },
  ],
  updated: [
    { value: "week", label: "This week" },
    { value: "month", label: "This month" },
    { value: "year", label: "This year" },
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
  "unknown",
];
const VALID_SETUP_TYPES: readonly SetupType[] = ["auto", "manual", "unknown"];
const VALID_UPDATED: readonly UpdatedRange[] = ["week", "month", "year"];

/**
 * Bridge the unified value object back to the catalog's `FilterValues` shape so
 * the existing `filterAndSortServers` logic is reused verbatim. Values are
 * sanitized to known options (invalid/absent collapse to the "any" sentinel:
 * 0 / "any").
 */
export function toCatalogFilterValues(
  values: FilterValues<typeof CATALOG_FILTERS>,
): CatalogFilterValues {
  return {
    authTypes: values.auth.filter((v): v is AuthType =>
      VALID_AUTH_TYPES.includes(v as AuthType),
    ),
    setupTypes: values.setup.filter((v): v is SetupType =>
      VALID_SETUP_TYPES.includes(v as SetupType),
    ),
    updatedRange: VALID_UPDATED.includes(values.updated as UpdatedRange)
      ? (values.updated as UpdatedRange)
      : "any",
  };
}

/** Whether any granular filter is active (drives chips + empty-state copy). */
export function hasActiveCatalogFilters(filters: CatalogFilterValues): boolean {
  return (
    filters.authTypes.length > 0 ||
    filters.setupTypes.length > 0 ||
    filters.updatedRange !== "any"
  );
}
