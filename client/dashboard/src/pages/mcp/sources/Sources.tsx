import { ResourceListPage } from "@/components/page-templates";
import {
  RemoveSourceDialog,
  type RemovableSource,
} from "@/components/sources/RemoveSourceDialog";
import { SourceCard } from "@/components/sources/source-grid";
import {
  sourceAssetId,
  useProjectSources,
} from "@/components/sources/source-list";
import { useSourceListActions } from "@/components/sources/source-list-actions";
import { DeploymentErrorsButton } from "@/components/sources/source-list-notices";
import { useSourceFailures } from "@/components/sources/source-list-failures";
import {
  SOURCE_FILTERS,
  SOURCE_FILTER_OPTIONS,
  matchesSourceFilters,
  matchesSourceSearch,
  sourceFacets,
  sourceFailureKey,
} from "@/components/sources/source-list-filters";
import { SourceTableRow } from "@/components/sources/source-table-row";
import { Button } from "@/components/ui/Button";
import { DotTable } from "@/components/ui/DotTable";
import { Text } from "@/components/ui/Text";
import { useViewMode } from "@/components/ui/ViewToggle/use-view-mode";
import { useProject } from "@/contexts/Auth";
import { useListTools } from "@/hooks/toolTypes";
import type { Tool } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { McpTabs } from "../McpTabs";
import { useFilterState, type FilterValue } from "@/components/filters";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";

export function SourcesRoot(): JSX.Element {
  return <Outlet />;
}

/** The deployment-asset id a tool was generated from, if it has one. */
function toolSourceId(tool: Tool): string | undefined {
  if (tool.type === "http") return tool.openapiv3DocumentId;
  if (tool.type === "function") return tool.functionId;
  return undefined;
}

/**
 * What this project has to build servers from.
 *
 * Sources lost their own section when MCP became the inventory, but the CLI
 * still hands people a link after a push and functions still arrive this way,
 * so they keep a place to land, with the acting done from the add flow next
 * door and from each source's own menu.
 */
export default function Sources(): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const { sources, isLoading } = useProjectSources();
  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode();
  const filters = useFilterState(SOURCE_FILTERS);
  const [removing, setRemoving] = useState<RemovableSource | null>(null);
  const actionsFor = useSourceListActions({ onRemove: setRemoving });

  // Usage runs through tool URNs: a hosted server is a toolset, and a
  // toolset names its tools under the source's prefix. A failed server list
  // reads as "not used" rather than taking the shelf down.
  const { data: toolsetsResult } = useListToolsets(undefined, undefined, {
    throwOnError: false,
  });
  const toolsetToolUrns = useMemo(
    () =>
      (toolsetsResult?.toolsets ?? []).flatMap(
        (toolset) => toolset.toolUrns ?? [],
      ),
    [toolsetsResult],
  );
  const { failingKeys, failedDeploymentId } = useSourceFailures();

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
      const id = toolSourceId(tool);
      if (id) counts.set(id, (counts.get(id) ?? 0) + 1);
    }
    return counts;
  }, [toolsResult]);

  const filtered = useMemo(
    () =>
      sources.filter(
        (source) =>
          matchesSourceSearch(source, search) &&
          matchesSourceFilters(
            sourceFacets(source, toolsetToolUrns, failingKeys),
            filters.values,
          ),
      ),
    [sources, search, toolsetToolUrns, failingKeys, filters.values],
  );

  // The failure mark carries the deployment it links to, so a source that
  // is fine gets no id and no mark.
  const failureFor = (source: (typeof sources)[number]) =>
    failingKeys.has(sourceFailureKey(source)) ? failedDeploymentId : undefined;

  return (
    <ResourceListPage
      scope="mcp:read"
      resourceId={project.id}
      title="Sources"
      description="The OpenAPI documents and functions this project deploys. Their tools are what an MCP server built from them starts with."
      belowHeader={<McpTabs active="sources" />}
      toolbarActions={
        <>
          <DeploymentErrorsButton failedDeploymentId={failedDeploymentId} />
          {/* h-10 matches the toolbar's own controls, as on the servers tab. */}
          <Button variant="primary" className="h-10" asChild>
            <routes.mcp.add.fromSource.Link>
              <Button.Text>Build a server</Button.Text>
            </routes.mcp.add.fromSource.Link>
          </Button>
        </>
      }
      search={{
        value: search,
        onChange: setSearch,
        placeholder: "Search sources...",
      }}
      filters={{
        schema: SOURCE_FILTERS,
        values: filters.values,
        optionsById: SOURCE_FILTER_OPTIONS,
        onChange: filters.setValue as (id: string, v: FilterValue) => void,
        onClear: filters.clearValue as (id: string) => void,
        onClearAll: filters.clearAll,
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
        action: (
          <Button variant="primary" asChild>
            <routes.mcp.add.Link>
              <Button.Text>Add a source</Button.Text>
            </routes.mcp.add.Link>
          </Button>
        ),
      }}
    >
      {filtered.length === 0 ? (
        // Distinct from the empty state above: the project has sources, this
        // search just doesn't match any, so the toolbar stays put.
        <Text muted small>
          No sources match the current search and filters.
        </Text>
      ) : null}
      {viewMode === "grid" && filtered.length > 0 && (
        <div className="@2xl/main:grid-cols-2 grid grid-cols-1 gap-4">
          {filtered.map((source) => (
            <SourceCard
              key={source.key}
              source={source}
              actions={actionsFor(source)}
              failedDeploymentId={failureFor(source)}
              // A source has an address of its own here, rather than a sheet:
              // this page is where someone is sent to look one up.
              onInspect={() =>
                routes.mcp.sources.detail.goTo(sourceAssetId(source))
              }
            />
          ))}
        </div>
      )}
      {viewMode === "table" && filtered.length > 0 && (
        <DotTable
          headers={[
            { label: "Name" },
            { label: "Kind" },
            { label: "Tools" },
            { label: "Created" },
            { label: "Updated" },
            { label: "" },
            { label: "", className: "text-right" },
          ]}
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
              actions={actionsFor(source)}
              failedDeploymentId={failureFor(source)}
            />
          ))}
        </DotTable>
      )}
      <RemoveSourceDialog
        source={removing}
        open={removing !== null}
        onOpenChange={(open) => {
          if (!open) setRemoving(null);
        }}
      />
    </ResourceListPage>
  );
}
