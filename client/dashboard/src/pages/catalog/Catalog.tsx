import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { DotTable } from "@/components/ui/dot-table";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useViewMode } from "@/components/ui/use-view-mode";
import { useProject } from "@/contexts/Auth";
import { AddServerDialog } from "@/pages/catalog/AddServerDialog";
import { CommandBar } from "@/pages/catalog/CommandBar";
import { type PulseMCPServer, useListMCPCatalog } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { Button, Stack } from "@/components/ui/moonshine";
import { SearchXIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import {
  useFilterState as useDimensionFilters,
  type FilterValue,
} from "@/components/filters";
import {
  CATALOG_FILTERS,
  CATALOG_FILTER_OPTIONS,
  hasActiveCatalogFilters,
  toCatalogFilterValues,
} from "./catalog-filter-schema";
import { filterAndSortServers } from "./hooks/serverMetadata";
import { useFilterState, type SortOption } from "./hooks/useFilterState";
import { useSelectionState } from "./hooks/useSelectionState";
import { ServerCard } from "./ServerCard";
import { ServerTableRow } from "./ServerTableRow";

// Sort options shown in the toolbar (mirrors the legacy SortDropdown order).
const CATALOG_SORT_OPTIONS: { value: SortOption; label: string }[] = [
  { value: "popular", label: "Most Popular" },
  { value: "recent", label: "Recently Released" },
  { value: "updated", label: "Last Updated" },
  { value: "alphabetical", label: "A → Z" },
  { value: "alphabetical-desc", label: "Z → A" },
];

export function CatalogRoot(): JSX.Element {
  return <Outlet />;
}

export default function Catalog(): JSX.Element {
  return (
    <RequireScope scope={["project:read", "mcp:write"]} level="page">
      <CatalogInner />
    </RequireScope>
  );
}

function CatalogInner() {
  const routes = useRoutes();
  const project = useProject();
  const [searchQuery, setSearchQuery] = useState("");

  // Category + sort stay page state (no UI to change category today; sort is the
  // SortDropdown). The five granular filters now run through the unified filter
  // system, which reads/writes the same URL params, so existing links keep
  // working. We bridge its value object back to the catalog's `FilterValues`
  // shape so `filterAndSortServers` is reused unchanged.
  const pageState = useFilterState();
  const dimensionFilters = useDimensionFilters(CATALOG_FILTERS);
  const filters = useMemo(
    () => toCatalogFilterValues(dimensionFilters.values),
    [dimensionFilters.values],
  );
  const filterState = useMemo(
    () => ({ category: pageState.category, sort: pageState.sort, filters }),
    [pageState.category, pageState.sort, filters],
  );

  // Selection state from URL (persists across navigation)
  const { selectedServers, toggleServerSelection, clearSelection } =
    useSelectionState();

  const [viewMode, setViewMode] = useViewMode();
  const [addingServers, setAddingServers] = useState<PulseMCPServer[]>([]);
  const [gridElement, setGridElement] = useState<HTMLDivElement | null>(null);

  const {
    data,
    isLoading,
    isFetching,
    refetch: refetchCatalog,
  } = useListMCPCatalog();
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;
  const externalMcps = deployment?.externalMcps ?? [];

  // The backend returns the full catalog in one response.
  const allServers = useMemo(
    () => (data?.servers as PulseMCPServer[]) ?? [],
    [data],
  );

  // Search, sort, and filter the full catalog client-side.
  const filteredServers = useMemo(() => {
    return filterAndSortServers(
      allServers,
      filterState,
      searchQuery || undefined,
    );
  }, [allServers, filterState, searchQuery]);

  // Check if any granular filters are active
  const hasActiveFilters = useMemo(
    () => hasActiveCatalogFilters(filters),
    [filters],
  );

  const getSelectedServerObjects = () =>
    filteredServers.filter((s) =>
      selectedServers.has(`${s.registryId}-${s.registrySpecifier}`),
    );

  const handleAdd = () => {
    setAddingServers(getSelectedServerObjects());
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>MCP Catalog</Page.Section.Title>
          <Page.Section.Description>
            Discover and import official third-party MCP servers to your
            project. Powered by the official{" "}
            <a
              href="https://www.speakeasy.com/product/mcp-gateway/catalog"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-foreground underline underline-offset-2"
            >
              MCP Registry
            </a>
            .
          </Page.Section.Description>
          <Page.Section.Body>
            <Stack direction="vertical" gap={6}>
              {/* Canonical toolbar: [search] [filters] [sort] … [count] [view]. */}
              <Page.Toolbar>
                <Page.Toolbar.Search
                  value={searchQuery}
                  onChange={setSearchQuery}
                  placeholder="Search MCP servers..."
                />
                <Page.Toolbar.Filters
                  schema={CATALOG_FILTERS}
                  values={dimensionFilters.values}
                  optionsById={CATALOG_FILTER_OPTIONS}
                  onChange={
                    dimensionFilters.setValue as (
                      id: string,
                      value: FilterValue,
                    ) => void
                  }
                  onClear={dimensionFilters.clearValue as (id: string) => void}
                  onClearAll={dimensionFilters.clearAll}
                />
                <Page.Toolbar.SortBy
                  value={pageState.sort}
                  onChange={(v) => pageState.setSort(v as SortOption)}
                  options={CATALOG_SORT_OPTIONS}
                />
                {!isLoading && (
                  <Page.Toolbar.Count>
                    {filteredServers.length === allServers.length
                      ? `${allServers.length} servers`
                      : `${filteredServers.length} of ${allServers.length} servers`}
                  </Page.Toolbar.Count>
                )}
                <Page.Toolbar.ViewAs value={viewMode} onChange={setViewMode} />
                <Page.Toolbar.Refresh
                  onRefresh={() => void refetchCatalog()}
                  isRefreshing={isFetching}
                />
              </Page.Toolbar>

              {/* Server grid / table */}
              {isLoading ? (
                <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
                  {Array.from({ length: 6 }, (_, i) => `skeleton-${i}`).map(
                    (id) => (
                      <Skeleton key={id} className="h-[200px]" />
                    ),
                  )}
                </div>
              ) : viewMode === "grid" ? (
                <div
                  ref={setGridElement}
                  className="grid grid-cols-1 gap-6 xl:grid-cols-2"
                >
                  {filteredServers.map((server) => {
                    const serverKey = `${server.registryId}-${server.registrySpecifier}`;
                    return (
                      <ServerCard
                        key={serverKey}
                        server={server}
                        detailHref={routes.catalog.detail.href(
                          encodeURIComponent(server.registrySpecifier),
                        )}
                        externalMcps={externalMcps}
                        isSelected={selectedServers.has(serverKey)}
                        onToggleSelect={() => toggleServerSelection(serverKey)}
                      />
                    );
                  })}
                </div>
              ) : (
                <div ref={setGridElement}>
                  <DotTable
                    headers={[
                      { label: "", className: "w-10" },
                      { label: "Name" },
                      { label: "Version" },
                      { label: "Description" },
                      { label: "Tools" },
                      { label: "" },
                    ]}
                  >
                    {filteredServers.map((server) => {
                      const serverKey = `${server.registryId}-${server.registrySpecifier}`;
                      return (
                        <ServerTableRow
                          key={serverKey}
                          server={server}
                          detailHref={routes.catalog.detail.href(
                            encodeURIComponent(server.registrySpecifier),
                          )}
                          externalMcps={externalMcps}
                          isSelected={selectedServers.has(serverKey)}
                          onToggleSelect={() =>
                            toggleServerSelection(serverKey)
                          }
                        />
                      );
                    })}
                  </DotTable>
                </div>
              )}

              {/* Empty state */}
              {!isLoading && filteredServers.length === 0 && (
                <EmptySearchResult
                  hasFilters={
                    hasActiveFilters ||
                    filterState.category !== "all" ||
                    searchQuery !== ""
                  }
                  onClear={() => {
                    setSearchQuery("");
                    // clearFilters resets category + sort + every filter param in
                    // a single URL update (the unified state re-reads from it),
                    // so a category-filtered empty state isn't left stuck.
                    pageState.clearFilters();
                  }}
                />
              )}
            </Stack>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>

      <AddServerDialog
        servers={addingServers}
        projectSlug={project.slug}
        open={addingServers.length > 0}
        onOpenChange={(open) => {
          if (!open) {
            setAddingServers([]);
            clearSelection();
          }
        }}
        onServersAdded={() => {
          void refetchDeployment();
        }}
      />
      <CommandBar
        selectedCount={selectedServers.size}
        onAdd={handleAdd}
        onClear={clearSelection}
        containerElement={gridElement}
      />
    </Page>
  );
}

function EmptySearchResult({
  hasFilters,
  onClear,
}: {
  hasFilters: boolean;
  onClear: () => void;
}) {
  return (
    <div className="bg-background flex w-full items-center justify-center border py-8">
      <Stack
        gap={1}
        className="m-8 w-full max-w-sm"
        align="center"
        justify="center"
      >
        <div className="py-6">
          <SearchXIcon className="text-foreground size-16" />
        </div>
        <Heading variant="h5" className="font-medium">
          No matching servers
        </Heading>
        <Type small muted className="mb-4 text-center">
          {hasFilters
            ? "No MCP servers match your current filters. Try adjusting or clearing your filters."
            : "No MCP servers found. Check back later for new additions."}
        </Type>
        {hasFilters && (
          <Button onClick={onClear} size="sm">
            <Button.Text>Clear Filters</Button.Text>
          </Button>
        )}
      </Stack>
    </div>
  );
}
