import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { DotTable } from "@/components/ui/DotTable";
import { Heading } from "@/components/ui/Heading";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useViewMode } from "@/components/ui/ViewToggle/use-view-mode";
import { useOrganization, useProject } from "@/contexts/Auth";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useShadowMCPPolicyInventory } from "@/components/shadow-mcp/useShadowMCPPolicyInventory";
import { useRBAC } from "@/hooks/useRBAC";
import { AddServerDialog } from "@/pages/catalog/AddServerDialog";
import { useSdkClient } from "@/contexts/Sdk";
import { invalidateAllMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  type CatalogServer,
  useCatalogInstallations,
  useListMCPCatalog,
} from "@/pages/catalog/hooks";
import { McpTabs } from "@/pages/mcp/McpTabs";
import { useRoutes } from "@/routes";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { SearchXIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet, useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";
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
import { ServerCard } from "./ServerCard";
import { ServerTableRow } from "./ServerTableRow";
import { TeamUsageTable } from "./TeamUsageTable";
import {
  CATALOG_SHOW_PARAM,
  CATALOG_SHOW_TEAM,
  type CatalogShow,
  teamUsedServers,
} from "./team-usage";

// Sort options shown in the toolbar (mirrors the legacy SortDropdown order).
const CATALOG_SORT_OPTIONS: { value: SortOption; label: string }[] = [
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
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  // Deep link from a gateway's Add member sheet: servers installed here get
  // attached to that gateway as members, and the flow returns to it.
  const [searchParams, setSearchParams] = useSearchParams();
  const attachToGatewayId = searchParams.get("attachToGateway");

  // The team view reads the org's Shadow MCP inventory, which only org admins
  // may see, so everyone else gets the catalog alone and never fires the call.
  // It is also left out while adding members to a gateway, a flow about the
  // catalog's own entries.
  const organization = useOrganization();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canSeeTeamUsage =
    !rbacLoading &&
    !attachToGatewayId &&
    hasScope("org:admin", organization.id);
  const show: CatalogShow =
    canSeeTeamUsage &&
    searchParams.get(CATALOG_SHOW_PARAM) === CATALOG_SHOW_TEAM
      ? CATALOG_SHOW_TEAM
      : "all";
  const setShow = (next: CatalogShow) => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        if (next === CATALOG_SHOW_TEAM) {
          params.set(CATALOG_SHOW_PARAM, CATALOG_SHOW_TEAM);
          return params;
        }
        params.delete(CATALOG_SHOW_PARAM);
        return params;
      },
      { replace: true },
    );
  };
  const teamInventory = useShadowMCPPolicyInventory(
    project.id,
    show === CATALOG_SHOW_TEAM,
  );
  const teamServers = useMemo(
    () => teamUsedServers(teamInventory.data ?? []),
    [teamInventory.data],
  );

  const attachInstalledToGateway = async (result: {
    status: "succeeded" | "failed";
    completedMcpServerIds?: string[];
  }) => {
    if (!attachToGatewayId) return;
    let attachFailures = 0;
    for (const mcpServerId of result.completedMcpServerIds ?? []) {
      try {
        await client.metaMcp.addMember({
          addMetaMcpMemberForm: {
            metaMcpServerId: attachToGatewayId,
            mcpServerId,
          },
        });
      } catch (err) {
        console.error("failed to attach installed server to gateway", err);
        attachFailures += 1;
      }
    }
    await invalidateAllMetaMcpMembers(queryClient);
    if (attachFailures > 0) {
      toast.error(
        attachFailures === 1
          ? "An installed server could not be added to the gateway. Add it from the gateway's overview."
          : `${attachFailures} installed servers could not be added to the gateway. Add them from the gateway's overview.`,
      );
    }
    // Redirect only when everything worked; otherwise stay so the dialog's
    // per-server results (and the toast above) remain visible.
    if (result.status === "succeeded" && attachFailures === 0) {
      void navigate(routes.mcp.gateway.overview.href(attachToGatewayId));
    }
  };

  // Sort stays page state (the SortDropdown). The granular filters run through
  // the unified filter system, which reads/writes the same URL params, so
  // existing links keep working. We bridge its value object back to the catalog's `FilterValues`
  // shape so `filterAndSortServers` is reused unchanged.
  const pageState = useFilterState();
  const dimensionFilters = useDimensionFilters(CATALOG_FILTERS);
  const filters = useMemo(
    () => toCatalogFilterValues(dimensionFilters.values),
    [dimensionFilters.values],
  );
  const filterState = useMemo(
    () => ({ sort: pageState.sort, filters }),
    [pageState.sort, filters],
  );

  const [viewMode, setViewMode] = useViewMode();
  const [addingServers, setAddingServers] = useState<CatalogServer[]>([]);

  const {
    data,
    isLoading,
    isFetching,
    refetch: refetchCatalog,
  } = useListMCPCatalog();
  const { installState } = useCatalogInstallations();

  // The backend returns the full catalog in one response.
  const allServers = useMemo(
    () => (data?.servers as CatalogServer[]) ?? [],
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

  // One server at a time: the card's own Add button opens the install dialog
  // for that server. The dialog still takes a list, so a future bulk path can
  // reuse it unchanged.
  const handleAdd = (server: CatalogServer) => {
    setAddingServers([server]);
  };

  return (
    <>
      <ResourceListPage
        title="MCP Catalog"
        description={
          <>
            Discover and import third-party MCP servers to your project. Powered
            by the{" "}
            <a
              href="https://www.speakeasy.com/product/mcp-gateway/catalog"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-foreground underline underline-offset-2"
            >
              MCP Registry
            </a>
            .
          </>
        }
        belowHeader={<McpTabs active="catalog" />}
        search={{
          value: searchQuery,
          onChange: setSearchQuery,
          placeholder: "Search MCP servers...",
        }}
        toolbarActions={
          canSeeTeamUsage ? (
            <SegmentedControl<CatalogShow>
              ariaLabel="Which servers to show"
              value={show}
              onChange={setShow}
              options={[
                {
                  value: "all",
                  label: "All servers",
                  tooltip: "Every server in the catalog",
                },
                {
                  value: CATALOG_SHOW_TEAM,
                  label: "Used by your team",
                  tooltip:
                    "Remote servers your team's agents connect to directly, most used first",
                },
              ]}
            />
          ) : undefined
        }
        filters={
          show === CATALOG_SHOW_TEAM
            ? undefined
            : {
                schema: CATALOG_FILTERS,
                values: dimensionFilters.values,
                optionsById: CATALOG_FILTER_OPTIONS,
                onChange: dimensionFilters.setValue as (
                  id: string,
                  value: FilterValue,
                ) => void,
                onClear: dimensionFilters.clearValue as (id: string) => void,
                onClearAll: dimensionFilters.clearAll,
              }
        }
        sort={
          show === CATALOG_SHOW_TEAM
            ? undefined
            : {
                value: pageState.sort,
                onChange: (v) => pageState.setSort(v as SortOption),
                options: CATALOG_SORT_OPTIONS,
              }
        }
        viewToggle={
          show === CATALOG_SHOW_TEAM
            ? undefined
            : { value: viewMode, onChange: setViewMode }
        }
        onRefresh={() => {
          if (show === CATALOG_SHOW_TEAM) {
            void teamInventory.refetch();
            return;
          }
          void refetchCatalog();
        }}
        isRefreshing={
          show === CATALOG_SHOW_TEAM ? teamInventory.isFetching : isFetching
        }
      >
        {show === CATALOG_SHOW_TEAM ? (
          <TeamUsageTable
            servers={teamServers}
            catalog={allServers}
            search={searchQuery}
            isLoading={teamInventory.isPending}
            isError={teamInventory.isError}
            onRetry={() => void teamInventory.refetch()}
            onAddFromCatalog={handleAdd}
          />
        ) : isLoading ? (
          <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
            {Array.from({ length: 6 }, (_, i) => `skeleton-${i}`).map((id) => (
              <Skeleton key={id} className="h-[200px]" />
            ))}
          </div>
        ) : viewMode === "grid" ? (
          <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
            {filteredServers.map((server) => {
              const serverKey = `${server.registryId}-${server.registrySpecifier}`;
              return (
                <ServerCard
                  key={serverKey}
                  server={server}
                  detailHref={routes.mcp.catalog.detail.href(
                    encodeURIComponent(server.registrySpecifier),
                  )}
                  installState={installState(server)}
                  onAdd={() => handleAdd(server)}
                />
              );
            })}
          </div>
        ) : (
          <div>
            <DotTable
              headers={[
                { label: "", className: "w-10" },
                { label: "Name" },
                { label: "Version" },
                { label: "Description" },
                { label: "" },
              ]}
            >
              {filteredServers.map((server) => {
                const serverKey = `${server.registryId}-${server.registrySpecifier}`;
                return (
                  <ServerTableRow
                    key={serverKey}
                    server={server}
                    detailHref={routes.mcp.catalog.detail.href(
                      encodeURIComponent(server.registrySpecifier),
                    )}
                    installState={installState(server)}
                    onAdd={() => handleAdd(server)}
                  />
                );
              })}
            </DotTable>
          </div>
        )}

        {/* Empty state */}
        {show === "all" && !isLoading && filteredServers.length === 0 && (
          <EmptySearchResult
            hasFilters={hasActiveFilters || searchQuery !== ""}
            onClear={() => {
              setSearchQuery("");
              // clearFilters resets sort + every filter param in a single
              // URL update (the unified state re-reads from it).
              pageState.clearFilters();
            }}
          />
        )}
      </ResourceListPage>

      <AddServerDialog
        openProviderSetup={!attachToGatewayId}
        attachToGatewayId={attachToGatewayId ?? undefined}
        servers={addingServers}
        projectSlug={project.slug}
        open={addingServers.length > 0}
        onOpenChange={(open) => {
          if (!open) setAddingServers([]);
        }}
        onInstallFinished={
          attachToGatewayId
            ? (result) => void attachInstalledToGateway(result)
            : undefined
        }
      />
    </>
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
        <Text small muted className="mb-4 text-center">
          {hasFilters
            ? "No MCP servers match your current filters. Try adjusting or clearing your filters."
            : "No MCP servers found. Check back later for new additions."}
        </Text>
        {hasFilters && (
          <Button onClick={onClear} size="sm">
            <Button.Text>Clear Filters</Button.Text>
          </Button>
        )}
      </Stack>
    </div>
  );
}
