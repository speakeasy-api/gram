import { ResourceListPage } from "@/components/page-templates";
import { SourceCard } from "@/components/sources/source-grid";
import {
  sourceAssetId,
  useProjectSources,
} from "@/components/sources/source-list";
import { SourceTableRow } from "@/components/sources/source-table-row";
import { Button } from "@/components/ui/Button";
import { DotTable } from "@/components/ui/DotTable";
import { Text } from "@/components/ui/Text";
import { useViewMode } from "@/components/ui/ViewToggle/use-view-mode";
import { useProject } from "@/contexts/Auth";
import { useListTools } from "@/hooks/toolTypes";
import { useRoutes } from "@/routes";
import { McpTabs } from "../McpTabs";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";

// The one thing that distinguishes one source from another at a glance, and
// the only facet the deployment carries for them.
const SOURCE_FILTERS = defineFilters([
  { id: "kind", label: "Kind", kind: "multiselect" },
]);

const SOURCE_FILTER_OPTIONS: OptionsById = {
  kind: [
    { value: "openapi", label: "OpenAPI document" },
    { value: "function", label: "Function" },
  ],
};

export function SourcesRoot(): JSX.Element {
  return <Outlet />;
}

/**
 * What this project has to build servers from.
 *
 * Sources lost their own section when MCP became the inventory, but the CLI
 * still hands people a link after a push and functions still arrive this way,
 * so they keep a place to land — a read-only shelf, with the acting done from
 * the add flow next door.
 */
export default function Sources(): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const { sources, isLoading } = useProjectSources();
  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode();
  const kindFilters = useFilterState(SOURCE_FILTERS);

  // The table shows how many tools each source produced. Cards leave that to
  // the detail page, so the fetch only matters in table view, but it is the
  // same query the detail page makes and is cached across the two.
  const { data: toolsResult } = useListTools(undefined, undefined, {
    enabled: viewMode === "table",
  });
  const toolCounts = useMemo(() => {
    if (!toolsResult) return undefined;
    const counts = new Map<string, number>();
    for (const tool of toolsResult.tools) {
      const id =
        tool.type === "http"
          ? tool.openapiv3DocumentId
          : tool.type === "function"
            ? tool.functionId
            : undefined;
      if (id) counts.set(id, (counts.get(id) ?? 0) + 1);
    }
    return counts;
  }, [toolsResult]);

  const selectedKinds = kindFilters.values["kind"];
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    const kinds = Array.isArray(selectedKinds) ? selectedKinds : [];
    return sources.filter((source) => {
      if (query && !source.name.toLowerCase().includes(query)) return false;
      if (kinds.length > 0 && !kinds.includes(source.kind)) return false;
      return true;
    });
  }, [sources, search, selectedKinds]);

  return (
    <ResourceListPage
      scope="mcp:read"
      resourceId={project.id}
      title="Sources"
      description="The OpenAPI documents and functions this project deploys. Their tools are what an MCP server built from them starts with."
      belowHeader={<McpTabs active="sources" />}
      toolbarActions={
        // h-10 matches the toolbar's own controls, as on the servers tab.
        <Button variant="primary" className="h-10" asChild>
          <routes.mcp.add.fromSource.Link>
            <Button.Text>Build a server</Button.Text>
          </routes.mcp.add.fromSource.Link>
        </Button>
      }
      search={{
        value: search,
        onChange: setSearch,
        placeholder: "Search sources...",
      }}
      filters={{
        schema: SOURCE_FILTERS,
        values: kindFilters.values,
        optionsById: SOURCE_FILTER_OPTIONS,
        onChange: kindFilters.setValue as (id: string, v: FilterValue) => void,
        onClear: kindFilters.clearValue as (id: string) => void,
        onClearAll: kindFilters.clearAll,
      }}
      viewToggle={{ value: viewMode, onChange: setViewMode }}
      isLoading={isLoading}
      isEmpty={sources.length === 0}
      hideToolbar={sources.length === 0}
      empty={{
        icon: "file-code",
        heading: "No sources yet",
        description:
          "Push an OpenAPI document or a function, from the CLI or the add flow, and it shows up here.",
      }}
    >
      {filtered.length === 0 ? (
        // Distinct from the empty state above: the project has sources, this
        // search just doesn't match any, so the toolbar stays put.
        <Text muted small>
          No sources match the current search and filters.
        </Text>
      ) : null}
      {viewMode === "grid" ? (
        <div className="@2xl/main:grid-cols-2 grid grid-cols-1 gap-4">
          {filtered.map((source) => (
            <SourceCard
              key={source.key}
              source={source}
              // A source has an address of its own here, rather than a sheet:
              // this page is where someone is sent to look one up.
              onInspect={() =>
                routes.mcp.sources.detail.goTo(sourceAssetId(source))
              }
            />
          ))}
        </div>
      ) : filtered.length > 0 ? (
        <DotTable
          headers={[{ label: "Name" }, { label: "Kind" }, { label: "Tools" }]}
        >
          {filtered.map((source) => (
            <SourceTableRow
              key={source.key}
              source={source}
              toolCount={
                toolCounts
                  ? (toolCounts.get(sourceAssetId(source)) ?? 0)
                  : undefined
              }
              href={routes.mcp.sources.detail.href(sourceAssetId(source))}
            />
          ))}
        </DotTable>
      ) : null}
    </ResourceListPage>
  );
}
