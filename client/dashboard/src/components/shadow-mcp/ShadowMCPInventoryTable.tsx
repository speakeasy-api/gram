import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { Page } from "@/components/page-layout";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useShadowMCPInventory } from "@gram/client/react-query/shadowMCPInventory.js";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { type Column, type SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { useEffect, useMemo, useState } from "react";
import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { cn } from "@/lib/utils";
import {
  type DecideAccessTarget,
  DecideAccessSheet,
} from "@/components/mcp-approvals/DecideAccessSheet";
import {
  ShadowMCPInventoryReviewCell,
  ShadowMCPInventoryServerCell,
  ShadowMCPInventoryUsageCell,
} from "./ShadowMCPInventoryCells";
import { ReviewRequestSheet } from "@/components/mcp-approvals/ReviewRequestSheet";
import {
  defineFilters,
  type FilterValue,
  useFilterState,
} from "@/components/filters";
import {
  shadowMCPBlockingPolicyDisposition,
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
  type ShadowMCPPolicy,
  type ShadowMCPPolicyDisposition,
  type ShadowMCPPolicyState,
} from "./shadowMCPInventoryStatus";

const REVIEW_FILTER_OPTIONS = [
  { value: "requested", label: "Awaiting decision" },
  { value: "approved", label: "Approved" },
  { value: "denied", label: "Denied" },
  { value: "unreviewed", label: "Review initiated" },
  { value: "none", label: "No review" },
];

const INVENTORY_FILTERS = defineFilters([
  { id: "review", label: "Review", kind: "select" },
]);

/**
 * A seen-time is real only when telemetry produced it: synthesized
 * review-only rows carry the zero time, which must read as never observed
 * rather than as January of year one.
 */
function observedDate(date: Date | undefined): Date | undefined {
  if (!date || date.getTime() <= 0) return undefined;
  return date;
}

/** Pending decisions sort first; the rest follow the review lifecycle. */
function reviewSortRank(server: ShadowMCPInventoryServer): number {
  switch (server.approvalRequest?.status) {
    case "requested":
      return 0;
    case "unreviewed":
      return 2;
    case "approved":
    case "denied":
      return 1;
    case undefined:
      return 3;
  }
}

function matchesReviewFilter(
  server: ShadowMCPInventoryServer,
  review: string | undefined,
): boolean {
  if (!review) return true;
  if (review === "none") return server.approvalRequest === undefined;
  return server.approvalRequest?.status === review;
}

const INVENTORY_PAGE_LIMIT = 50;
const FIRST_PAGE_CURSOR = "";

type InventoryPage = {
  cursor: string;
  nextCursor?: string;
  servers: ShadowMCPInventoryServer[];
};

const EMPTY_INVENTORY_PAGES: InventoryPage[] = [];

function InventoryStatusCell({
  disposition,
  policyState,
  server,
}: {
  disposition: ShadowMCPPolicyDisposition | null;
  policyState: ShadowMCPPolicyState;
  server: ShadowMCPInventoryServer;
}) {
  const status = shadowMCPInventoryStatus(server, policyState);

  return (
    <div className="space-y-1">
      <Badge variant={shadowMCPInventoryStatusBadgeVariant(status)}>
        <Badge.Text>{shadowMCPInventoryStatusLabel(status)}</Badge.Text>
      </Badge>
      <Text variant="small" className="text-muted-foreground text-xs">
        {shadowMCPInventoryStatusDescription(server, policyState, disposition)}
      </Text>
    </div>
  );
}

function InventoryEmptyState() {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center border border-dashed px-8 py-16 text-center">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name="shield-check" className="text-muted-foreground h-6 w-6" />
      </div>
      <Text variant="subheading" className="mb-1">
        No Shadow MCP servers
      </Text>
      <Text small muted className="mb-4 max-w-md">
        Inventory URLs will appear here after hook startup captures configured
        Shadow MCP servers.
      </Text>
    </div>
  );
}

export function ShadowMCPInventoryTable({
  className,
  enabled = true,
  members,
  onOpenServer,
  policyState,
  projectID,
  roles,
  shadowMCPPolicies,
}: {
  className?: string;
  enabled?: boolean;
  members: AccessMember[];
  onOpenServer?: (server: ShadowMCPInventoryServer) => void;
  policyState: ShadowMCPPolicyState;
  projectID: string;
  roles: Role[];
  shadowMCPPolicies: ShadowMCPPolicy[];
}): JSX.Element {
  const inventoryScope = enabled && projectID.length > 0 ? projectID : "";
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [pages, setPages] = useState<InventoryPage[]>([]);
  const [paginationScope, setPaginationScope] = useState(inventoryScope);
  const hasActivePagination = paginationScope === inventoryScope;
  const activeCursor = hasActivePagination ? cursor : undefined;
  const activePages = hasActivePagination ? pages : EMPTY_INVENTORY_PAGES;
  const inventoryRequest = activeCursor
    ? {
        projectId: projectID,
        limit: INVENTORY_PAGE_LIMIT,
        cursor: activeCursor,
      }
    : { projectId: projectID, limit: INVENTORY_PAGE_LIMIT };
  const inventoryQuery = useShadowMCPInventory(inventoryRequest, undefined, {
    enabled: enabled && projectID.length > 0,
  });
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "review",
    direction: "asc",
  });
  const [decideTarget, setDecideTarget] = useState<DecideAccessTarget | null>(
    null,
  );
  const [reviewSheetServer, setReviewSheetServer] =
    useState<ShadowMCPInventoryServer | null>(null);
  const { values, setValue, clearValue, clearAll } =
    useFilterState(INVENTORY_FILTERS);
  const disposition = shadowMCPBlockingPolicyDisposition(shadowMCPPolicies);

  useEffect(() => {
    setPaginationScope(inventoryScope);
    setCursor(undefined);
    setPages([]);
  }, [inventoryScope]);

  useEffect(() => {
    if (
      !hasActivePagination ||
      !enabled ||
      projectID.length === 0 ||
      !inventoryQuery.data
    ) {
      return;
    }

    const pageCursor = activeCursor ?? FIRST_PAGE_CURSOR;
    setPages((currentPages) => {
      const page: InventoryPage = {
        cursor: pageCursor,
        nextCursor: inventoryQuery.data.nextCursor,
        servers: inventoryQuery.data.servers,
      };
      const existingPageIndex = currentPages.findIndex(
        (currentPage) => currentPage.cursor === pageCursor,
      );

      if (existingPageIndex === -1) {
        return [...currentPages, page];
      }

      return currentPages.map((currentPage, index) =>
        index === existingPageIndex ? page : currentPage,
      );
    });
  }, [
    activeCursor,
    enabled,
    hasActivePagination,
    inventoryQuery.data,
    projectID,
  ]);

  const loadedServers = useMemo(() => {
    return activePages.flatMap((page) => page.servers);
  }, [activePages]);

  const latestPage = activePages[activePages.length - 1];
  const canUseInventoryQueryData =
    enabled && projectID.length > 0 && hasActivePagination;
  const nextCursor =
    latestPage?.nextCursor ??
    (canUseInventoryQueryData ? inventoryQuery.data?.nextCursor : undefined);
  const hasLoadedPages = activePages.length > 0;
  const isInitialLoading = inventoryQuery.isLoading && !hasLoadedPages;
  const isInitialError = Boolean(inventoryQuery.error && !hasLoadedPages);
  const isLoadingMore = Boolean(
    hasLoadedPages && (inventoryQuery.isFetching || inventoryQuery.isLoading),
  );

  const loadMoreServers = () => {
    if (!nextCursor || isLoadingMore) {
      return;
    }

    if (activeCursor === nextCursor && inventoryQuery.error) {
      void inventoryQuery.refetch();
      return;
    }

    setCursor(nextCursor);
  };

  const isStdio = (server: ShadowMCPInventoryServer) =>
    server.targetKind === "stdio_command";

  const openRow = (server: ShadowMCPInventoryServer) => {
    // Local commands have no server page; their review lives in a sheet.
    if (isStdio(server)) {
      setReviewSheetServer(server);
      return;
    }
    onOpenServer?.(server);
  };

  const openDecide = (server: ShadowMCPInventoryServer) => {
    if (isStdio(server)) {
      setReviewSheetServer(server);
      return;
    }
    setDecideTarget({
      canonicalServerUrl: server.canonicalServerUrl,
      displayName:
        server.serverName || server.urlHost || server.canonicalServerUrl,
      approvalRequestId: server.approvalRequest?.id,
      // A pending legacy bypass request rides along so the sheet promotes it
      // into the review and the decision drains it too.
      pendingBypassRequestId: server.latestRequest?.id,
    });
  };

  const columns: Column<ShadowMCPInventoryServer>[] = [
    {
      key: "server",
      header: "Server",
      sortable: true,
      sortValue: (server) =>
        (server.serverName || server.urlHost || server.canonicalServerUrl)
          .trim()
          .toLowerCase(),
      width: "2fr",
      render: (server) => <ShadowMCPInventoryServerCell server={server} />,
    },
    {
      key: "status",
      header: "Status",
      sortable: true,
      sortValue: (server) =>
        shadowMCPInventoryStatusLabel(
          shadowMCPInventoryStatus(server, policyState),
        ),
      width: "0.9fr",
      render: (server) =>
        server.targetKind === "stdio_command" ? (
          <Text muted small>
            —
          </Text>
        ) : (
          <InventoryStatusCell
            disposition={disposition}
            policyState={policyState}
            server={server}
          />
        ),
    },
    {
      key: "review",
      header: "Review",
      sortable: true,
      sortValue: reviewSortRank,
      width: "0.8fr",
      render: (server) => <ShadowMCPInventoryReviewCell server={server} />,
    },
    {
      key: "lastCalled",
      header: "Last called",
      sortable: true,
      sortValue: (server) => server.lastCalled?.getTime() ?? 0,
      width: "0.7fr",
      render: (server) => (
        <Text variant="small">{formatShortDate(server.lastCalled)}</Text>
      ),
    },
    {
      key: "lastSeen",
      header: "Last seen",
      sortable: true,
      sortValue: (server) => observedDate(server.lastSeen)?.getTime() ?? 0,
      width: "0.7fr",
      render: (server) => (
        <Text variant="small">
          {formatShortDate(observedDate(server.lastSeen))}
        </Text>
      ),
    },
    {
      key: "usage",
      header: "Usage",
      sortable: true,
      sortValue: (server) => server.observedUseCount,
      width: "0.5fr",
      render: (server) => <ShadowMCPInventoryUsageCell server={server} />,
    },
  ];

  const servers = useMemo(() => {
    if (loadedServers.length > 0) {
      return loadedServers;
    }

    return canUseInventoryQueryData ? (inventoryQuery.data?.servers ?? []) : [];
  }, [canUseInventoryQueryData, inventoryQuery.data?.servers, loadedServers]);
  const normalizedSearch = search.trim().toLowerCase();
  const reviewFilter = values.review ?? undefined;
  const filteredServers = useMemo(() => {
    return servers.filter((server) => {
      if (!matchesReviewFilter(server, reviewFilter)) return false;
      if (normalizedSearch.length === 0) return true;
      return [
        server.serverName,
        server.urlHost,
        server.canonicalServerUrl,
      ].some((value) => value?.toLowerCase().includes(normalizedSearch));
    });
  }, [normalizedSearch, reviewFilter, servers]);
  const sortedServers = sortTableData(
    filteredServers,
    columns,
    sort,
  ) as ShadowMCPInventoryServer[];
  const reviewFilterLabel = REVIEW_FILTER_OPTIONS.find(
    (option) => option.value === reviewFilter,
  )?.label;
  const noResultsMessage =
    normalizedSearch.length > 0
      ? `No servers matching “${search.trim()}”`
      : reviewFilterLabel
        ? `No servers with review state “${reviewFilterLabel}”`
        : undefined;

  if (isInitialLoading) {
    return <SkeletonTable />;
  }

  if (isInitialError) {
    return (
      <div className="bg-background flex min-h-32 flex-col items-center justify-center gap-1 px-4 py-8 text-center">
        <Text variant="body" className="font-medium">
          Shadow MCP inventory could not be loaded
        </Text>
        <Text muted small className="max-w-md">
          Refresh the page or try again later.
        </Text>
      </div>
    );
  }

  if (servers.length === 0) {
    return <InventoryEmptyState />;
  }

  return (
    <div
      className={cn(
        "flex min-h-0 shrink flex-col gap-4 overflow-hidden",
        className,
      )}
    >
      <ReviewRequestSheet
        request={
          reviewSheetServer?.approvalRequest
            ? {
                id: reviewSheetServer.approvalRequest.id,
                targetRaw: reviewSheetServer.canonicalServerUrl,
                requesterCount:
                  reviewSheetServer.approvalRequest.requesterCount,
              }
            : null
        }
        open={reviewSheetServer !== null}
        onOpenChange={(open) => {
          if (!open) {
            setReviewSheetServer(null);
          }
        }}
      />
      <DecideAccessSheet
        target={decideTarget}
        open={decideTarget !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDecideTarget(null);
          }
        }}
        disposition={disposition}
        members={members}
        roles={roles}
        onDecided={() => {
          setCursor(undefined);
          setPages([]);
        }}
      />
      <Page.Toolbar className="shrink-0">
        <Page.Toolbar.Search
          onChange={setSearch}
          placeholder="Search servers..."
          value={search}
        />
        <Page.Toolbar.Filters
          schema={INVENTORY_FILTERS}
          values={values}
          optionsById={{ review: REVIEW_FILTER_OPTIONS }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
      </Page.Toolbar>
      <Table
        columns={columns}
        className="min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)] overflow-x-auto overflow-y-hidden"
      >
        <Table.Header columns={columns} sort={sort} onSortChange={setSort} />
        <Table.Body
          columns={columns}
          data={sortedServers}
          handleLoadMore={loadMoreServers}
          hasMore={Boolean(nextCursor)}
          isLoading={isLoadingMore}
          noResultsMessage={noResultsMessage}
          onRowClick={openRow}
          rowKey={(row) => row.canonicalServerUrl}
          className="min-h-0 content-start overflow-y-auto"
          renderRow={(row, rowElement) => (
            <TableRowContextMenu
              key={row.canonicalServerUrl}
              actions={[
                {
                  label: "Decide access",
                  onClick: () => openDecide(row),
                },
              ]}
            >
              {rowElement}
            </TableRowContextMenu>
          )}
        />
      </Table>
    </div>
  );
}
