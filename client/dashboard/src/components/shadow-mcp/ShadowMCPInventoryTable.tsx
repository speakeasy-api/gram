import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useDeleteShadowMCPInventoryPolicyBypassMutation } from "@gram/client/react-query/deleteShadowMCPInventoryPolicyBypass.js";
import { useResolveShadowMCPInventoryRequestMutation } from "@gram/client/react-query/resolveShadowMCPInventoryRequest.js";
import {
  invalidateAllShadowMCPInventory,
  useShadowMCPInventory,
} from "@gram/client/react-query/shadowMCPInventory.js";
import { useUpsertShadowMCPInventoryPolicyBypassMutation } from "@gram/client/react-query/upsertShadowMCPInventoryPolicyBypass.js";
import {
  Badge,
  type Column,
  Icon,
  type SortDescriptor,
  Table,
  sortTableData,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { cn } from "@/lib/utils";
import {
  type ActiveInventoryAction,
  type ReviewDecision,
  ShadowMCPInventoryActionMenu,
  ShadowMCPInventoryActionSheet,
  type ShadowMCPPolicy,
} from "./ShadowMCPInventoryActions";
import { shadowMCPInventoryActions } from "./shadowMCPInventoryActionItems";
import {
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
  type ShadowMCPPolicyState,
} from "./shadowMCPInventoryStatus";

const INVENTORY_PAGE_LIMIT = 50;
const FIRST_PAGE_CURSOR = "";

type InventoryPage = {
  cursor: string;
  nextCursor?: string;
  servers: ShadowMCPInventoryServer[];
};

const EMPTY_INVENTORY_PAGES: InventoryPage[] = [];

function usageCountLabel(count: number) {
  return `${count} ${count === 1 ? "call" : "calls"}`;
}

function userCountLabel(count: number) {
  return `${count} ${count === 1 ? "user" : "users"}`;
}

function InventoryServerCell({ server }: { server: ShadowMCPInventoryServer }) {
  const label = server.serverName || server.urlHost;

  return (
    <div className="min-w-0 space-y-1">
      <div className="flex gap-2 items-center">
        <Type variant="small" className="truncate font-medium">
          {label}
        </Type>
        {server.requestCount > 0 && (
          <Badge variant="warning" size="sm" background={false}>
            <Badge.LeftIcon>
              <Icon name="shield-alert" />
            </Badge.LeftIcon>
            <Badge.Text>
              {server.requestCount} Access Request
              {server.requestCount > 1 && "s"}
            </Badge.Text>
          </Badge>
        )}
      </div>
      <Type variant="small" className="text-muted-foreground truncate text-xs">
        {server.canonicalServerUrl}
      </Type>
    </div>
  );
}

function InventoryStatusCell({
  policyState,
  server,
}: {
  policyState: ShadowMCPPolicyState;
  server: ShadowMCPInventoryServer;
}) {
  const status = shadowMCPInventoryStatus(server, policyState);

  return (
    <div className="space-y-1">
      <Badge variant={shadowMCPInventoryStatusBadgeVariant(status)}>
        <Badge.Text>{shadowMCPInventoryStatusLabel(status)}</Badge.Text>
      </Badge>
      <Type variant="small" className="text-muted-foreground text-xs">
        {shadowMCPInventoryStatusDescription(server, policyState)}
      </Type>
    </div>
  );
}

function UsageCell({ server }: { server: ShadowMCPInventoryServer }) {
  return (
    <div className="space-y-1">
      <Type variant="small">{usageCountLabel(server.observedUseCount)}</Type>
      <Type variant="small" className="text-muted-foreground text-xs">
        {userCountLabel(server.userCount)}
      </Type>
    </div>
  );
}

function InventoryEmptyState() {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16 text-center">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name="shield-check" className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No Shadow MCP servers
      </Type>
      <Type small muted className="mb-4 max-w-md">
        Inventory URLs will appear here after hook startup captures configured
        Shadow MCP servers.
      </Type>
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
  const queryClient = useQueryClient();
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
  const upsertPolicyBypass = useUpsertShadowMCPInventoryPolicyBypassMutation();
  const deletePolicyBypass = useDeleteShadowMCPInventoryPolicyBypassMutation();
  const resolveInventoryRequest = useResolveShadowMCPInventoryRequestMutation();
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "lastCalled",
    direction: "desc",
  });
  const [activeAction, setActiveAction] =
    useState<ActiveInventoryAction | null>(null);
  const [isSubmittingAction, setIsSubmittingAction] = useState(false);
  const isSubmitting =
    isSubmittingAction ||
    upsertPolicyBypass.isPending ||
    deletePolicyBypass.isPending ||
    resolveInventoryRequest.isPending;
  const isActionPending = isSubmitting || activeAction !== null;

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

  const refreshInventory = async () => {
    setCursor(undefined);
    setPages([]);
    await invalidateAllShadowMCPInventory(queryClient);
  };

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

  const submitInventoryAction = async ({
    action,
    decision,
    policyIDs,
  }: {
    action: ActiveInventoryAction;
    decision: ReviewDecision;
    policyIDs: string[];
  }) => {
    const label = action.server.serverName ?? action.server.canonicalServerUrl;
    setIsSubmittingAction(true);
    try {
      if (action.mode === "delete") {
        await deletePolicyBypass.mutateAsync({
          request: {
            projectId: projectID,
            serverUrl: action.server.canonicalServerUrl,
          },
        });
        toast.success(`Removed allow rule for: ${label}`);
      } else if (action.mode === "review") {
        await resolveInventoryRequest.mutateAsync({
          request: {
            resolveShadowMCPInventoryRequestForm: {
              decision,
              policyIds: decision === "allow" ? policyIDs : undefined,
              projectId: projectID,
              serverUrl: action.server.canonicalServerUrl,
            },
          },
        });
        toast.success(
          decision === "allow"
            ? `Request approved for: ${label}`
            : `Request denied for: ${label}`,
        );
      } else {
        await upsertPolicyBypass.mutateAsync({
          request: {
            shadowMCPInventoryPolicyBypassForm: {
              policyIds: policyIDs,
              projectId: projectID,
              serverUrl: action.server.canonicalServerUrl,
            },
          },
        });
        toast.success(`Allow rule saved for: ${label}`);
      }
      await refreshInventory();
      setActiveAction(null);
    } catch {
      toast.error(`Unable to update allow rule for: ${label}`);
    } finally {
      setIsSubmittingAction(false);
    }
  };

  const renderActionsCell = (server: ShadowMCPInventoryServer) => {
    return (
      <ShadowMCPInventoryActionMenu
        disabled={isActionPending}
        onOpenAction={(mode, selectedServer) =>
          setActiveAction({ mode, server: selectedServer })
        }
        server={server}
      />
    );
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
      render: (server) => <InventoryServerCell server={server} />,
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
      render: (server) => (
        <InventoryStatusCell policyState={policyState} server={server} />
      ),
    },
    {
      key: "lastCalled",
      header: "Last called",
      sortable: true,
      sortValue: (server) => server.lastCalled?.getTime() ?? 0,
      width: "0.7fr",
      render: (server) => (
        <Type variant="small">{formatShortDate(server.lastCalled)}</Type>
      ),
    },
    {
      key: "lastSeen",
      header: "Last seen",
      sortable: true,
      sortValue: (server) => server.lastSeen.getTime(),
      width: "0.7fr",
      render: (server) => (
        <Type variant="small">{formatShortDate(server.lastSeen)}</Type>
      ),
    },
    {
      key: "usage",
      header: "Usage",
      sortable: true,
      sortValue: (server) => server.observedUseCount,
      width: "0.5fr",
      render: (server) => <UsageCell server={server} />,
    },
    {
      key: "actions",
      header: "",
      width: "0.3fr",
      render: renderActionsCell,
    },
  ];

  const servers =
    loadedServers.length > 0
      ? loadedServers
      : canUseInventoryQueryData
        ? (inventoryQuery.data?.servers ?? [])
        : [];
  const sortedServers = sortTableData(
    servers,
    columns,
    sort,
  ) as ShadowMCPInventoryServer[];

  if (isInitialLoading) {
    return <SkeletonTable />;
  }

  if (isInitialError) {
    return (
      <div className="bg-background flex min-h-32 flex-col items-center justify-center gap-1 px-4 py-8 text-center">
        <Type variant="body" className="font-medium">
          Shadow MCP inventory could not be loaded
        </Type>
        <Type muted small className="max-w-md">
          Refresh the page or try again later.
        </Type>
      </div>
    );
  }

  if (servers.length === 0) {
    return <InventoryEmptyState />;
  }

  return (
    <div className={cn("min-h-0 shrink overflow-hidden", className)}>
      <ShadowMCPInventoryActionSheet
        action={activeAction}
        isSubmitting={isSubmitting}
        members={members}
        onOpenChange={(open) => {
          if (!open) {
            setActiveAction(null);
          }
        }}
        onSubmit={submitInventoryAction}
        open={activeAction !== null}
        roles={roles}
        shadowMCPPolicies={shadowMCPPolicies}
      />
      <Table
        columns={columns}
        className="h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-x-auto overflow-y-hidden"
      >
        <Table.Header columns={columns} sort={sort} onSortChange={setSort} />
        <Table.Body
          columns={columns}
          data={sortedServers}
          handleLoadMore={loadMoreServers}
          hasMore={Boolean(nextCursor)}
          isLoading={isLoadingMore}
          onRowClick={onOpenServer}
          rowKey={(row) => row.canonicalServerUrl}
          className="min-h-0 content-start overflow-y-auto"
          renderRow={(row, rowElement) => (
            <TableRowContextMenu
              key={row.canonicalServerUrl}
              actions={shadowMCPInventoryActions(row, {
                disabled: isActionPending,
                onOpenAction: (mode, selectedServer) =>
                  setActiveAction({ mode, server: selectedServer }),
              })}
            >
              {rowElement}
            </TableRowContextMenu>
          )}
        />
      </Table>
    </div>
  );
}
