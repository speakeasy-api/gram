import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { DotTable } from "@/components/ui/dot-table";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { ViewToggle } from "@/components/ui/view-toggle";
import { useViewMode } from "@/components/ui/use-view-mode";
import { useProject } from "@/contexts/Auth";
import { AddServerDialog } from "@/pages/catalog/AddServerDialog";
import { CommandBar } from "@/pages/catalog/CommandBar";
import {
  type PulseMCPServer,
  useInfiniteListMCPCatalog,
} from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query";
import { Button, Input, Stack } from "@speakeasy-api/moonshine";
import { Loader2, Search, SearchXIcon, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Outlet } from "react-router";
import { FilterChips } from "./FilterChips";
import { defaultFilterValues } from "./filter-defaults";
import { FilterSidebar } from "./FilterSidebar";
import { filterAndSortServers } from "./hooks/serverMetadata";
import { useFilterState } from "./hooks/useFilterState";
import { useSelectionState } from "./hooks/useSelectionState";
import { ServerCard } from "./ServerCard";
import { ServerTableRow } from "./ServerTableRow";
import { SortDropdown } from "./SortDropdown";

export function CatalogRoot() {
  return <Outlet />;
}

export default function Catalog() {
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

  // Filter state from URL
  const filterState = useFilterState();

  // Selection state from URL (persists across navigation)
  const { selectedServers, toggleServerSelection, clearSelection } =
    useSelectionState();

  const [viewMode, setViewMode] = useViewMode();
  const [addingServers, setAddingServers] = useState<PulseMCPServer[]>([]);
  const [gridElement, setGridElement] = useState<HTMLDivElement | null>(null);

  // Track if we've loaded all data (for client-side search)
  const [allDataLoaded, setAllDataLoaded] = useState(false);

  // Only use server-side search if we haven't loaded all data yet
  // Normalize empty string to undefined for consistent query keys
  const serverSideSearch = allDataLoaded ? undefined : searchQuery || undefined;

  const {
    data,
    isLoading,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    debouncedSearch,
  } = useInfiniteListMCPCatalog(serverSideSearch);
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;
  const externalMcps = deployment?.externalMcps ?? [];
  const loadMoreRef = useRef<HTMLDivElement>(null);

  // Track when all data has been loaded (for the unfiltered query only)
  // Once we've loaded all data without a search, we can switch to client-side filtering
  useEffect(() => {
    // Only set allDataLoaded based on the unfiltered (no search) query state
    // Use debouncedSearch (not searchQuery) to ensure we're checking against the actual
    // query state, avoiding timing mismatches during debounce transitions
    if (
      !debouncedSearch &&
      !hasNextPage &&
      !isLoading &&
      data?.pages &&
      data.pages.length > 0
    ) {
      setAllDataLoaded(true);
    }
    // Never reset allDataLoaded to false - once we have all data, we keep it
  }, [hasNextPage, isLoading, data?.pages, debouncedSearch]);

  // Flatten all pages into a single list
  const allServers = useMemo(() => {
    return (
      data?.pages.flatMap((page) => page.servers as PulseMCPServer[]) ?? []
    );
  }, [data]);

  // Apply client-side filtering based on filter state
  // Use client-side search when all data is loaded
  const clientSideSearch = allDataLoaded ? searchQuery : undefined;
  const filteredServers = useMemo(() => {
    return filterAndSortServers(allServers, filterState, clientSideSearch);
  }, [allServers, filterState, clientSideSearch]);

  // Check if any granular filters are active
  const hasActiveFilters = useMemo(() => {
    const f = filterState.filters;
    return (
      f.authTypes.length > 0 ||
      f.toolBehaviors.length > 0 ||
      f.minUsers > 0 ||
      f.updatedRange !== "any" ||
      f.minTools > 0
    );
  }, [filterState.filters]);

  const getSelectedServerObjects = () =>
    filteredServers.filter((s) =>
      selectedServers.has(`${s.registryId}-${s.registrySpecifier}`),
    );

  const handleAdd = () => {
    setAddingServers(getSelectedServerObjects());
  };

  // Infinite scroll with IntersectionObserver
  useEffect(() => {
    const element = loadMoreRef.current;
    if (!element) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasNextPage && !isFetchingNextPage) {
          fetchNextPage();
        }
      },
      { threshold: 0.1 },
    );

    observer.observe(element);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage]);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>MCP Catalog</Page.Section.Title>
          <Page.Section.Description>
            Discover and import MCP servers to your project. Powered by the
            official MCP registry.
          </Page.Section.Description>
          <Page.Section.Body>
            <Stack direction="vertical" gap={6}>
              {/* Search and filters row */}
              <Stack
                direction="horizontal"
                gap={3}
                align="center"
                justify="space-between"
              >
                <Stack direction="horizontal" gap={3} align="center">
                  <div className="relative w-64">
                    <Search className="text-muted-foreground absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2" />
                    <Input
                      placeholder="Search MCP servers..."
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      className="h-10 pr-9 pl-10"
                    />
                    {searchQuery && (
                      <button
                        onClick={() => setSearchQuery("")}
                        className="text-muted-foreground hover:text-foreground absolute top-1/2 right-3 -translate-y-1/2 transition-colors"
                        aria-label="Clear search"
                      >
                        <X className="h-4 w-4" />
                      </button>
                    )}
                  </div>
                  <FilterSidebar
                    values={filterState.filters}
                    onChange={filterState.setFilters}
                    onClear={() => filterState.setFilters(defaultFilterValues)}
                  />
                  <SortDropdown
                    value={filterState.sort}
                    onChange={filterState.setSort}
                  />
                </Stack>

                {/* Results count and view toggle */}
                <Stack direction="horizontal" gap={3} align="center">
                  {!isLoading && (
                    <Type small muted>
                      {filteredServers.length === allServers.length
                        ? `${allServers.length} servers`
                        : `${filteredServers.length} of ${allServers.length} servers`}
                    </Type>
                  )}
                  <ViewToggle value={viewMode} onChange={setViewMode} />
                </Stack>
              </Stack>

              {/* Active filter chips */}
              {hasActiveFilters && (
                <FilterChips
                  values={filterState.filters}
                  onChange={filterState.setFilters}
                  onClearAll={() => filterState.setFilters(defaultFilterValues)}
                />
              )}

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

              {/* Load more trigger */}
              {!isLoading && hasNextPage && !searchQuery && (
                <div
                  ref={loadMoreRef}
                  className="flex items-center justify-center py-8"
                >
                  {isFetchingNextPage && (
                    <Stack direction="horizontal" gap={2} align="center">
                      <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
                      <Type small muted>
                        Loading more...
                      </Type>
                    </Stack>
                  )}
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
                    filterState.clearFilters();
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
        onServersAdded={() => refetchDeployment()}
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
    <div className="bg-background flex w-full items-center justify-center rounded-xl border py-8">
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
