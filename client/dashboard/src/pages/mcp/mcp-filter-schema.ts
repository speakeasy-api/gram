import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import {
  defineFilters,
  type FilterValues,
  type OptionsById,
} from "@/components/filters";

// No auth facet: hosted list rows lack issuer/OAuth fields, so filtering would hide valid rows.
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
    { value: "tunneled", label: "Tunneled" },
  ],
};

export interface McpFacets {
  status: "public" | "private" | "disabled";
  source: "catalog" | "custom" | "remote" | "tunneled";
}

export function toolsetFacets(toolset: ToolsetEntry): McpFacets {
  const status = !toolset.mcpEnabled
    ? "disabled"
    : toolset.mcpIsPublic
      ? "public"
      : "private";
  // registrySpecifier is the only list-row signal that a hosted MCP came from catalog.
  const source = toolset.origin?.registrySpecifier ? "catalog" : "custom";
  return { status, source };
}

export function mcpServerFacets(server: McpServer): McpFacets {
  const status =
    server.visibility === "public"
      ? "public"
      : server.visibility === "private"
        ? "private"
        : "disabled";
  return {
    status,
    source: server.tunneledMcpServerId ? "tunneled" : "remote",
  };
}

export function matchesMcpFilters(
  facets: McpFacets,
  values: FilterValues<typeof MCP_FILTERS>,
): boolean {
  return (
    (values.status.length === 0 || values.status.includes(facets.status)) &&
    (values.source.length === 0 || values.source.includes(facets.source))
  );
}

export function hasActiveMcpFilters(
  values: FilterValues<typeof MCP_FILTERS>,
): boolean {
  return values.status.length > 0 || values.source.length > 0;
}
