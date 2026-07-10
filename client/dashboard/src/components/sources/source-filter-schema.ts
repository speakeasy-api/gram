import {
  defineFilters,
  type FilterDimension,
  type FilterOption,
  type FilterValues,
  type OptionsById,
} from "@/components/filters";
import type { SourceType } from "@/lib/sources";

/**
 * Filters for the Sources listing.
 *
 * `transport`, `format`, and `catalogKind` are *conditional*: each only makes
 * sense for one source type, so it is hidden until that type is selected. The
 * filter system has no built-in notion of conditional dimensions, so
 * visibility is derived here (see {@link visibleSourceFilters}) and the same
 * predicate gates matching — otherwise a stale `?transport=sse` left in the URL
 * would keep narrowing the list after its parent type was deselected, with no
 * chip on screen to explain why.
 */
export const SOURCE_FILTERS = defineFilters([
  { id: "type", label: "Type", kind: "multiselect" },
  {
    id: "usedInMcp",
    label: "MCP usage",
    kind: "select",
    allLabel: "Any MCP usage",
  },
  { id: "transport", label: "Transport", kind: "multiselect" },
  { id: "format", label: "Format", kind: "multiselect" },
  { id: "catalogKind", label: "Catalog kind", kind: "multiselect" },
  { id: "failing", label: "Deployment errors", kind: "boolean" },
]);

export type SourceFilterValues = FilterValues<typeof SOURCE_FILTERS>;

/** Each conditional dimension and the source type that reveals it. */
const CONDITIONAL_ON: Record<string, SourceType> = {
  transport: "remotemcp",
  format: "openapi",
  catalogKind: "externalmcp",
};

function isDimensionVisible(id: string, values: SourceFilterValues): boolean {
  const requiredType = CONDITIONAL_ON[id];
  return requiredType === undefined || values.type.includes(requiredType);
}

/** The schema to hand `Page.Toolbar.Filters`, minus dimensions that don't apply. */
export function visibleSourceFilters(
  values: SourceFilterValues,
): FilterDimension[] {
  return SOURCE_FILTERS.filter((dim) => isDimensionVisible(dim.id, values));
}

const SOURCE_TYPE_OPTIONS: FilterOption[] = [
  { value: "openapi", label: "OpenAPI" },
  { value: "function", label: "Function" },
  { value: "externalmcp", label: "Catalog" },
  { value: "remotemcp", label: "Remote MCP" },
  { value: "tunneledmcp", label: "Tunneled MCP" },
];

/**
 * The Type options, minus `tunneledmcp` when the tunneled-MCP feature flag is
 * off — those sources aren't fetched, so offering the option would just filter
 * to an empty list.
 */
export function sourceTypeFilterOptions(
  includeTunneled: boolean,
): FilterOption[] {
  return includeTunneled
    ? SOURCE_TYPE_OPTIONS
    : SOURCE_TYPE_OPTIONS.filter((o) => o.value !== "tunneledmcp");
}

export const SOURCE_FILTER_OPTIONS: OptionsById = {
  type: SOURCE_TYPE_OPTIONS,
  usedInMcp: [
    { value: "yes", label: "Used in an MCP server" },
    { value: "no", label: "Not used in any MCP server" },
  ],
  format: [
    { value: "json", label: "JSON" },
    { value: "yaml", label: "YAML" },
  ],
  catalogKind: [
    { value: "server", label: "Server" },
    { value: "collection", label: "Collection" },
  ],
  // `transport` is supplied at render time — the set of transports in use is
  // data, not a fixed enum.
};

const TRANSPORT_LABELS: Record<string, string> = {
  "streamable-http": "Streamable HTTP",
  sse: "SSE",
};

/** Options for the transport dimension, derived from the transports actually in use. */
export function transportFilterOptions(transports: string[]): FilterOption[] {
  return Array.from(new Set(transports))
    .sort()
    .map((transport) => ({
      value: transport,
      label: TRANSPORT_LABELS[transport] ?? transport,
    }));
}

export interface SourceFacets {
  type: SourceType;
  /** Whether any MCP server (hosted toolset or mcp_server row) exposes this source. */
  usedInMcp: boolean;
  /** Remote MCP only. */
  transport?: string | undefined;
  /** OpenAPI only. */
  format?: "json" | "yaml" | undefined;
  /** Catalog only. */
  catalogKind?: "server" | "collection" | undefined;
  failing: boolean;
}

/** Map an asset's content type onto the coarse format facet. */
export function contentTypeToFormat(
  contentType: string | undefined,
): "json" | "yaml" | undefined {
  if (!contentType) return undefined;
  if (contentType.includes("yaml") || contentType.includes("yml"))
    return "yaml";
  if (contentType.includes("json")) return "json";
  return undefined;
}

export function matchesSourceFilters(
  facets: SourceFacets,
  values: SourceFilterValues,
): boolean {
  if (values.type.length > 0 && !values.type.includes(facets.type)) {
    return false;
  }

  // Only the declared "yes"/"no" values filter; a stale or malformed URL param
  // (e.g. ?usedInMcp=maybe) is ignored rather than silently treated as "no".
  if (values.usedInMcp === "yes" || values.usedInMcp === "no") {
    const wantUsed = values.usedInMcp === "yes";
    if (facets.usedInMcp !== wantUsed) return false;
  }

  if (values.failing && !facets.failing) return false;

  if (isDimensionVisible("transport", values) && values.transport.length > 0) {
    // Non-remote sources have no transport, so an active transport filter
    // excludes them — the same way selecting a type narrows the list.
    if (!facets.transport || !values.transport.includes(facets.transport)) {
      return false;
    }
  }

  if (isDimensionVisible("format", values) && values.format.length > 0) {
    if (!facets.format || !values.format.includes(facets.format)) return false;
  }

  if (
    isDimensionVisible("catalogKind", values) &&
    values.catalogKind.length > 0
  ) {
    if (!facets.catalogKind || !values.catalogKind.includes(facets.catalogKind))
      return false;
  }

  return true;
}

export function hasActiveSourceFilters(values: SourceFilterValues): boolean {
  return SOURCE_FILTERS.some((dim) => {
    if (!isDimensionVisible(dim.id, values)) return false;
    const value = values[dim.id as keyof SourceFilterValues];
    if (Array.isArray(value)) return value.length > 0;
    if (typeof value === "boolean") return value;
    return value !== null;
  });
}
