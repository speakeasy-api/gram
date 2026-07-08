import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useDeleteShadowMCPInventoryAllowRuleMutation } from "@gram/client/react-query/deleteShadowMCPInventoryAllowRule.js";
import {
  invalidateAllShadowMCPInventory,
  useShadowMCPInventory,
} from "@gram/client/react-query/shadowMCPInventory.js";
import { useUpsertShadowMCPInventoryAllowRuleMutation } from "@gram/client/react-query/upsertShadowMCPInventoryAllowRule.js";
import {
  Badge,
  Button,
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
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
  return (
    <div className="min-w-0 space-y-1">
      <Type variant="small" className="truncate font-medium">
        {server.serverName || server.urlHost}
      </Type>
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
  blockingPolicyIDs,
  className,
  enabled = true,
  policyState,
  projectID,
}: {
  blockingPolicyIDs: string[];
  className?: string;
  enabled?: boolean;
  policyState: ShadowMCPPolicyState;
  projectID: string;
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
  const upsertAllowRule = useUpsertShadowMCPInventoryAllowRuleMutation();
  const deleteAllowRule = useDeleteShadowMCPInventoryAllowRuleMutation();
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "lastCalled",
    direction: "desc",
  });
  const [pendingServerURL, setPendingServerURL] = useState<string | null>(null);
  const isMutating = upsertAllowRule.isPending || deleteAllowRule.isPending;
  const isActionPending = isMutating || pendingServerURL !== null;

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
  const requestCountForServer = (server: ShadowMCPInventoryServer) =>
    server.requestCount;

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

  const allowInventoryServer = async (server: ShadowMCPInventoryServer) => {
    setPendingServerURL(server.canonicalServerUrl);
    const label = server.serverName ?? server.canonicalServerUrl;
    try {
      if (blockingPolicyIDs.length === 0) {
        throw new Error("No blocking Shadow MCP policy is available");
      }
      await upsertAllowRule.mutateAsync({
        request: {
          shadowMCPInventoryAllowRuleForm: {
            policyIds: blockingPolicyIDs,
            projectId: projectID,
            serverUrl: server.canonicalServerUrl,
          },
        },
      });
      await refreshInventory();
      toast.success(`Allow rule added for: ${label}`);
    } catch {
      toast.error(`Unable to add allow rule for: ${label}`);
    } finally {
      setPendingServerURL(null);
    }
  };

  const clearInventoryServer = async (server: ShadowMCPInventoryServer) => {
    setPendingServerURL(server.canonicalServerUrl);
    const label = server.serverName ?? server.canonicalServerUrl;
    try {
      await deleteAllowRule.mutateAsync({
        request: {
          projectId: projectID,
          serverUrl: server.canonicalServerUrl,
        },
      });
      await refreshInventory();
      toast.success(`Removed allow rule for: ${label}`);
    } catch {
      toast.error(`Unable to remove allow rule for: ${label}`);
    } finally {
      setPendingServerURL(null);
    }
  };

  const renderRuleActionCell = (server: ShadowMCPInventoryServer) => {
    const isServerPending = pendingServerURL === server.canonicalServerUrl;
    const label = server.serverName || server.urlHost;
    const hasAccessRule = server.access === "allowed";
    const buttonLabel = hasAccessRule ? "Clear" : "Allow";
    const iconName = hasAccessRule ? "minus" : "plus";
    const tooltip = hasAccessRule
      ? `Clear Access Rule for ${label}`
      : `Add Access Rule for ${label}`;

    return (
      <Tooltip delayDuration={300}>
        <TooltipTrigger asChild>
          <Button
            size="xs"
            variant="secondary"
            disabled={isActionPending}
            onClick={() => {
              if (hasAccessRule) {
                void clearInventoryServer(server);
              } else {
                void allowInventoryServer(server);
              }
            }}
          >
            <Button.LeftIcon>
              {isServerPending ? (
                <Icon name="loader-circle" className="animate-spin" />
              ) : (
                <Icon name={iconName} />
              )}
            </Button.LeftIcon>
            <Button.Text>{buttonLabel}</Button.Text>
          </Button>
        </TooltipTrigger>
        <TooltipContent>{tooltip}</TooltipContent>
      </Tooltip>
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
      width: "1.7fr",
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
      width: "0.8fr",
      render: (server) => (
        <InventoryStatusCell policyState={policyState} server={server} />
      ),
    },
    {
      key: "lastCalled",
      header: "Last called",
      sortable: true,
      sortValue: (server) => server.lastCalled?.getTime() ?? 0,
      width: "0.85fr",
      render: (server) => (
        <Type variant="small">{formatShortDate(server.lastCalled)}</Type>
      ),
    },
    {
      key: "lastSeen",
      header: "Last seen",
      sortable: true,
      sortValue: (server) => server.lastSeen.getTime(),
      width: "0.85fr",
      render: (server) => (
        <Type variant="small">{formatShortDate(server.lastSeen)}</Type>
      ),
    },
    {
      key: "usage",
      header: "Usage",
      sortable: true,
      sortValue: (server) => server.observedUseCount,
      width: "0.7fr",
      render: (server) => (
        <div className="space-y-1">
          <Type variant="small">
            {usageCountLabel(server.observedUseCount)}
          </Type>
          <Type variant="small" className="text-muted-foreground text-xs">
            {userCountLabel(server.userCount)}
          </Type>
        </div>
      ),
    },
    {
      key: "requests",
      header: "Requests",
      sortable: true,
      sortValue: requestCountForServer,
      width: "0.6fr",
      render: (server) => {
        const count = requestCountForServer(server);
        if (count > 0) {
          return (
            <Badge variant="warning" background={false}>
              <Badge.LeftIcon>
                <Icon name="shield-alert" />
              </Badge.LeftIcon>
              <Badge.Text>{count}</Badge.Text>
            </Badge>
          );
        }
        return <Type variant="small">-</Type>;
      },
    },
    {
      key: "accessRule",
      header: "Access Rule",
      width: "0.5fr",
      render: renderRuleActionCell,
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
          Access Rules could not be loaded
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
          rowKey={(row) => row.canonicalServerUrl}
          className="min-h-0 content-start overflow-y-auto"
        />
      </Table>
    </div>
  );
}
