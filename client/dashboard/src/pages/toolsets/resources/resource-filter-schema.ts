import {
  defineFilters,
  type FilterValues,
  type OptionsById,
} from "@/components/filters";
import type { Resource } from "@/lib/toolTypes";

export const RESOURCE_FILTERS = defineFilters([
  { id: "type", label: "Type", kind: "multiselect" },
  { id: "usedInMcp", label: "Used in MCP", kind: "multiselect" },
]);

export const RESOURCE_FILTER_OPTIONS: OptionsById = {
  type: [
    { value: "function", label: "Function" },
    { value: "catalog", label: "Catalog" },
    { value: "remote", label: "Remote MCP" },
    { value: "openapi", label: "OpenAPI" },
  ],
  usedInMcp: [
    { value: "yes", label: "Used" },
    { value: "no", label: "Not used" },
  ],
};

export function resourceMatchesFilters(
  resource: Resource,
  values: FilterValues<typeof RESOURCE_FILTERS>,
  isUsed: boolean,
): boolean {
  const typeMatch =
    values.type.length === 0 || values.type.includes(resource.type);

  const usedMatch =
    values.usedInMcp.length === 0 ||
    values.usedInMcp.includes(isUsed ? "yes" : "no");

  return typeMatch && usedMatch;
}
