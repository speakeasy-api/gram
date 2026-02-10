import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { AddServersDialog } from "@/pages/catalog/AddServerDialog";
import { CommandBar } from "@/pages/catalog/CommandBar";
import { type Server, useInfiniteListMCPCatalog } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query";
import { Button, Input, Stack } from "@speakeasy-api/moonshine";
import { Loader2, Search, SearchXIcon } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { CategoryTabs } from "./CategoryTabs";
import { FilterChips } from "./FilterChips";
import { defaultFilterValues, FilterSidebar } from "./FilterSidebar";
import { countByCategory, filterAndSortServers } from "./hooks/serverMetadata";
import { useFilterState } from "./hooks/useFilterState";
import { ServerCard } from "./ServerCard";
import { SortDropdown } from "./SortDropdown";

export function CatalogRoot() {
  return <Outlet />;
}

export default function Catalog() {
  const routes = useRoutes();
  const client = useSdkClient();
  const organization = useOrganization();
  const project = useProject();
  const navigate = useNavigate();
  const [searchQuery, setSearchQuery] = useState("");

  // Filter state from URL
  const filterState = useFilterState();

  const [selectedServers, setSelectedServers] = useState<Set<string>>(
    new Set(),
  );
  const [addingServers, setAddingServers] = useState<Server[]>([]);
  const [targetProjectSlug, setTargetProjectSlug] = useState(project.slug);

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteListMCPCatalog(searchQuery);
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;
  const externalMcps = deployment?.externalMcps ?? [];
  const loadMoreRef = useRef<HTMLDivElement>(null);

  // Flatten all pages into a single list
  const allServers = useMemo(() => {
    return data?.pages.flatMap((page) => page.servers as Server[]) ?? [];
  }, [data]);

  // Count servers by category for badges
  const categoryCounts = useMemo(() => {
    return countByCategory(allServers);
  }, [allServers]);

  // Apply client-side filtering based on filter state
  const filteredServers = useMemo(() => {
    return filterAndSortServers(allServers, filterState);
  }, [allServers, filterState]);

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

  const toggleServerSelection = (serverKey: string) => {
    setSelectedServers((prev) => {
      const next = new Set(prev);
      if (next.has(serverKey)) {
        next.delete(serverKey);
      } else {
        next.add(serverKey);
      }
      return next;
    });
  };

  const clearSelection = () => setSelectedServers(new Set());

  const getSelectedServerObjects = () =>
    filteredServers.filter((s) =>
      selectedServers.has(`${s.registryId}-${s.registrySpecifier}`),
    );

  const projectOptions = organization.projects.map((p) => ({
    value: p.slug,
    label: p.name || p.slug,
  }));

  const handleAdd = () => {
    setAddingServers(getSelectedServerObjects());
  };

  const handleGoToMCPs = () => {
    navigate(`/${organization.slug}/${targetProjectSlug}/mcp`);
  };

  const handleCreateProject = async (name: string) => {
    const result = await client.projects.create({
      createProjectRequestBody: {
        name,
        organizationId: organization.id,
      },
    });
    await organization.refetch();
    setTargetProjectSlug(result.project.slug);
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
              {/* Search bar */}
              <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                <Input
                  placeholder="Search MCP servers..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="pl-10"
                />
              </div>

              {/* Category tabs + Filters row */}
              <Stack
                direction="horizontal"
                gap={4}
                align="center"
                justify="space-between"
                className="flex-wrap"
              >
                <CategoryTabs
                  value={filterState.category}
                  onChange={filterState.setCategory}
                  counts={categoryCounts}
                />

                <Stack direction="horizontal" gap={3} align="center">
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
              </Stack>

              {/* Results count */}
              {!isLoading && (
                <div className="flex justify-end">
                  <Type small muted>
                    {filteredServers.length === allServers.length
                      ? `${allServers.length} servers`
                      : `${filteredServers.length} of ${allServers.length} servers`}
                  </Type>
                </div>
              )}

              {/* Active filter chips */}
              {hasActiveFilters && (
                <FilterChips
                  values={filterState.filters}
                  onChange={filterState.setFilters}
                  onClearAll={() => filterState.setFilters(defaultFilterValues)}
                />
              )}

              {/* Server grid */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {isLoading &&
                  Array.from({ length: 6 }, (_, i) => `skeleton-${i}`).map(
                    (id) => <Skeleton key={id} className="h-[200px]" />,
                  )}
                {!isLoading &&
                  filteredServers.map((server) => {
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

              {/* Load more trigger */}
              {!isLoading && hasNextPage && !searchQuery && (
                <div
                  ref={loadMoreRef}
                  className="flex justify-center items-center py-8"
                >
                  {isFetchingNextPage && (
                    <Stack direction="horizontal" gap={2} align="center">
                      <Loader2 className="w-4 h-4 animate-spin text-muted-foreground" />
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

      <AddServersDialog
        servers={addingServers}
        projects={projectOptions}
        projectSlug={targetProjectSlug}
        onProjectChange={setTargetProjectSlug}
        onCreateProject={handleCreateProject}
        onGoToMCPs={handleGoToMCPs}
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
        projects={projectOptions}
        currentProjectSlug={targetProjectSlug}
        onSelectProject={setTargetProjectSlug}
        onCreateProject={handleCreateProject}
        onAdd={handleAdd}
        onClear={clearSelection}
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
    <div className="w-full flex items-center justify-center bg-background rounded-xl border py-8">
      <Stack
        gap={1}
        className="w-full max-w-sm m-8"
        align="center"
        justify="center"
      >
        <div className="py-6">
          <SearchXIcon className="size-16 text-foreground" />
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
