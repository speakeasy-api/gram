import type { McpServer, ToolsetEntry } from "@gram/client/models/components";
import {
  defineFilters,
  type FilterValues,
  type OptionsById,
} from "@/components/filters";

/**
 * Filters for the MCP listing, which merges two backend collections — Hosted
 * (toolset-backed) and Remote (`mcp_servers`) — into one grid. The two types
 * carry different fields, so each row is classified into a common set of facets
 * ({@link McpFacets}) and matched uniformly against the selected filter values.
 *
 * Auth was intentionally left out: the toolset *list* shape (`ToolsetEntry`)
 * does not carry `userSessionIssuerId`/`oauthEnablementMetadata` (only the full
 * `Toolset` detail does), so an auth filter couldn't be applied to Hosted rows
 * without silently mis-hiding them. Status + Source are available on both.
 *
 * Both dimensions are multiselect (OR within a dimension, AND across). Params
 * (`status`/`source`) are new, so they don't collide with anything the page
 * reads today.
 */
export const MCP_FILTERS = defineFilters([
  { id: "status", label: "Status", kind: "multiselect" },
  { id: "source", label: "Source", kind: "multiselect" },
]);

export const MCP_FILTER_OPTIONS: OptionsById = {
  status: [
    { value: "public", label: "Public" },
    { value: "private", label: "Private" },
    { value: "disabled", label: "Disabled" },
  ],
  source: [
    { value: "catalog", label: "Catalog" },
    { value: "custom", label: "Custom" },
    { value: "remote", label: "Remote URL" },
  ],
};

export interface McpFacets {
  status: "public" | "private" | "disabled";
  source: "catalog" | "custom" | "remote";
}

/** Classify a Hosted (toolset-backed) row. */
export function toolsetFacets(toolset: ToolsetEntry): McpFacets {
  const status = !toolset.mcpEnabled
    ? "disabled"
    : toolset.mcpIsPublic
      ? "public"
      : "private";
  // A registry specifier means the toolset was imported from the catalog;
  // otherwise it's built from the project's own sources (OpenAPI, functions).
  const source = toolset.origin?.registrySpecifier ? "catalog" : "custom";
  return { status, source };
}

/** Classify a Remote (`mcp_servers`-backed) row. */
export function mcpServerFacets(server: McpServer): McpFacets {
  const status =
    server.visibility === "public"
      ? "public"
      : server.visibility === "private"
        ? "private"
        : "disabled";
  return { status, source: "remote" };
}

/** Whether a classified row passes the active filter selection. */
export function matchesMcpFilters(
  facets: McpFacets,
  values: FilterValues<typeof MCP_FILTERS>,
): boolean {
  return (
    (values.status.length === 0 || values.status.includes(facets.status)) &&
    (values.source.length === 0 || values.source.includes(facets.source))
  );
}

/** Whether any MCP filter is active (drives empty-state copy + the bar). */
export function hasActiveMcpFilters(
  values: FilterValues<typeof MCP_FILTERS>,
): boolean {
  return values.status.length > 0 || values.source.length > 0;
}
