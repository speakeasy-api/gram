import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import {
  invalidateAllShadowMCPInventory,
  useShadowMCPInventory,
} from "@gram/client/react-query/shadowMCPInventory.js";
import { useAllowShadowMCPInventoryServerMutation } from "@gram/client/react-query/allowShadowMCPInventoryServer.js";
import { useBlockShadowMCPInventoryServerMutation } from "@gram/client/react-query/blockShadowMCPInventoryServer.js";
import { useClearShadowMCPInventoryServerAccessMutation } from "@gram/client/react-query/clearShadowMCPInventoryServerAccess.js";
import { Badge, Button, Column, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { formatShortDate } from "@/components/access/shadow-mcp-utils";

const INVENTORY_PAGE_LIMIT = 50;

function accessLabel(access: ShadowMCPInventoryServer["access"]) {
  switch (access) {
    case "allowed":
      return "Allowed";
    case "denied":
      return "Blocked";
    case "none":
      return "No rule";
  }
}

function accessBadgeVariant(access: ShadowMCPInventoryServer["access"]) {
  switch (access) {
    case "allowed":
      return "success" as const;
    case "denied":
      return "destructive" as const;
    case "none":
      return "neutral" as const;
  }
}

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

function AccessStateBadge({
  access,
}: {
  access: ShadowMCPInventoryServer["access"];
}) {
  return (
    <Badge variant={accessBadgeVariant(access)}>
      <Badge.Text>{accessLabel(access)}</Badge.Text>
    </Badge>
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
  enabled = true,
  projectID,
}: {
  enabled?: boolean;
  projectID: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const inventoryQuery = useShadowMCPInventory(
    { projectId: projectID, limit: INVENTORY_PAGE_LIMIT },
    undefined,
    { enabled: enabled && projectID.length > 0 },
  );
  const allowServer = useAllowShadowMCPInventoryServerMutation();
  const blockServer = useBlockShadowMCPInventoryServerMutation();
  const clearServer = useClearShadowMCPInventoryServerAccessMutation();
  const isMutating =
    allowServer.isPending || blockServer.isPending || clearServer.isPending;

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

  const blockInventoryServer = async (server: ShadowMCPInventoryServer) => {
    try {
      await blockServer.mutateAsync({
        request: {
          shadowMCPInventoryServerAccessForm: {
            projectId: projectID,
            serverName: server.serverName,
            serverUrl: server.canonicalServerUrl,
          },
        },
      });
      await refreshInventory();
      toast.success("Server blocked");
    } catch {
      toast.error("Server block failed");
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
      width: "1.7fr",
      render: (server) => <InventoryServerCell server={server} />,
    },
    {
      key: "access",
      header: "Access",
      width: "0.7fr",
      render: (server) => <AccessStateBadge access={server.access} />,
    },
    {
      key: "lastCalled",
      header: "Last called",
      width: "0.85fr",
      render: (server) => (
        <Type variant="small">{formatShortDate(server.lastCalled)}</Type>
      ),
    },
    {
      key: "lastSeen",
      header: "Last seen",
      width: "0.85fr",
      render: (server) => (
        <Type variant="small">{formatShortDate(server.lastSeen)}</Type>
      ),
    },
    {
      key: "usage",
      header: "Usage",
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
          {server.access !== "allowed" && (
            <Button
              size="sm"
              variant="secondary"
              disabled={isMutating}
              onClick={() => {
                void allowInventoryServer(server);
              }}
            >
              <Button.Text>Allow</Button.Text>
            </Button>
          )}
          {server.access !== "denied" && (
            <Button
              size="sm"
              variant="secondary"
              disabled={isMutating}
              onClick={() => {
                void blockInventoryServer(server);
              }}
            >
              <Button.Text>Block</Button.Text>
            </Button>
          )}
          {server.access !== "none" && (
            <Button
              size="sm"
              variant="secondary"
              disabled={isMutating}
              onClick={() => {
                void clearInventoryServer(server);
              }}
            >
              <Button.Text>Clear</Button.Text>
            </Button>
          )}
        </div>
      ),
    },
  ];

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

  const servers = inventoryQuery.data?.servers ?? [];
  if (servers.length === 0) {
    return <InventoryEmptyState />;
  }

  return (
    <div className="overflow-hidden rounded-lg border">
      <Table
        columns={columns}
        data={servers}
        rowKey={(row) => row.canonicalServerUrl}
        className="[&_thead]:bg-background max-h-128 overflow-y-auto rounded-none border-0 [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
      />
    </div>
  );
}
