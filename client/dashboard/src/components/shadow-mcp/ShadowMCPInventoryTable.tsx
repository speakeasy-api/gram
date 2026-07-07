import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import {
  invalidateAllShadowMCPInventory,
  useShadowMCPInventory,
} from "@gram/client/react-query/shadowMCPInventory.js";
import { useAllowShadowMCPInventoryServerMutation } from "@gram/client/react-query/allowShadowMCPInventoryServer.js";
import { useClearShadowMCPInventoryServerAccessMutation } from "@gram/client/react-query/clearShadowMCPInventoryServerAccess.js";
import {
  Badge,
  Button,
  type Column,
  type SortDescriptor,
  Table,
  sortTableData,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { ShieldCheck } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { cn } from "@/lib/utils";
import {
  shadowMCPInventoryActionLabel,
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
  type ShadowMCPPolicyState,
} from "./shadowMCPInventoryStatus";

const INVENTORY_PAGE_LIMIT = 50;

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
        <ShieldCheck className="text-muted-foreground h-6 w-6" />
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
  policyState,
  projectID,
}: {
  className?: string;
  enabled?: boolean;
  policyState: ShadowMCPPolicyState;
  projectID: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const inventoryQuery = useShadowMCPInventory(
    { projectId: projectID, limit: INVENTORY_PAGE_LIMIT },
    undefined,
    { enabled: enabled && projectID.length > 0 },
  );
  const allowServer = useAllowShadowMCPInventoryServerMutation();
  const clearServer = useClearShadowMCPInventoryServerAccessMutation();
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "lastSeen",
    direction: "desc",
  });
  const isMutating = allowServer.isPending || clearServer.isPending;

  const refreshInventory = async () => {
    await invalidateAllShadowMCPInventory(queryClient);
  };

  const allowInventoryServer = async (server: ShadowMCPInventoryServer) => {
    try {
      await allowServer.mutateAsync({
        request: {
          shadowMCPInventoryServerAccessForm: {
            projectId: projectID,
            serverName: server.serverName,
            serverUrl: server.canonicalServerUrl,
          },
        },
      });
      await refreshInventory();
      toast.success("Server allowed");
    } catch {
      toast.error("Server allow failed");
    }
  };

  const clearInventoryServer = async (server: ShadowMCPInventoryServer) => {
    try {
      await clearServer.mutateAsync({
        request: {
          clearShadowMCPInventoryServerAccessRequestBody: {
            projectId: projectID,
            serverUrl: server.canonicalServerUrl,
          },
        },
      });
      await refreshInventory();
      toast.success("Server access cleared");
    } catch {
      toast.error("Server access clear failed");
    }
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
      key: "actions",
      header: "",
      width: "1fr",
      render: (server) => (
        <div className="flex justify-end gap-2">
          {server.access === "allowed" ? (
            <Button
              size="sm"
              variant="secondary"
              disabled={isMutating}
              onClick={() => {
                void clearInventoryServer(server);
              }}
            >
              <Button.Text>{shadowMCPInventoryActionLabel(server)}</Button.Text>
            </Button>
          ) : (
            <Button
              size="sm"
              variant="secondary"
              disabled={isMutating}
              onClick={() => {
                void allowInventoryServer(server);
              }}
            >
              <Button.Text>{shadowMCPInventoryActionLabel(server)}</Button.Text>
            </Button>
          )}
        </div>
      ),
    },
  ];

  const servers = inventoryQuery.data?.servers ?? [];
  const sortedServers = sortTableData(
    servers,
    columns,
    sort,
  ) as ShadowMCPInventoryServer[];

  if (inventoryQuery.isLoading) {
    return <SkeletonTable />;
  }

  if (inventoryQuery.error) {
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
          rowKey={(row) => row.canonicalServerUrl}
          className="min-h-0 content-start overflow-y-auto"
        />
      </Table>
    </div>
  );
}
