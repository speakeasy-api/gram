import { InlineEmptyState } from "@/components/inline-empty-state";
import { Card } from "@/components/ui/Card";
import { MetricCard } from "@/components/ui/MetricCard";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { type Column, Table } from "@/components/ui/Table";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import {
  ShadowMCPInventoryServerCell,
  ShadowMCPInventoryUsageCell,
} from "./ShadowMCPInventoryCells";
import {
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
} from "./shadowMCPInventoryStatus";
import { useShadowMCPPolicyInventory } from "./useShadowMCPPolicyInventory";
import {
  shadowMCPPolicyUseCaseMetrics,
  shadowMCPRiskyServers,
} from "./shadowMCPUseCases";

const EMPTY_INVENTORY: ShadowMCPInventoryServer[] = [];

function statusColumn(server: ShadowMCPInventoryServer): JSX.Element {
  const status = shadowMCPInventoryStatus(server);
  return (
    <div className="space-y-1">
      <Badge variant={shadowMCPInventoryStatusBadgeVariant(status)}>
        <Badge.Text>{shadowMCPInventoryStatusLabel(status)}</Badge.Text>
      </Badge>
      <Text small muted className="text-xs">
        {shadowMCPInventoryStatusDescription(server)}
      </Text>
    </div>
  );
}

export function ShadowMCPPolicyUseCaseSection({
  compact = false,
  action,
}: {
  compact?: boolean;
  action?: { label: string; onClick: () => void };
}): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const navigate = useNavigate();
  const inventoryQuery = useShadowMCPPolicyInventory(project.id, true);
  const inventory = inventoryQuery.data ?? EMPTY_INVENTORY;
  const riskyServers = useMemo(
    () => shadowMCPRiskyServers(inventory, compact ? 5 : 8),
    [inventory, compact],
  );
  const { totalPendingRequests, restrictedOrBlockedCount } = useMemo(
    () => shadowMCPPolicyUseCaseMetrics(inventory),
    [inventory],
  );

  const columns: Column<ShadowMCPInventoryServer>[] = [
    {
      key: "server",
      header: "Server",
      width: "3fr",
      render: (server) => <ShadowMCPInventoryServerCell server={server} />,
    },
    {
      key: "status",
      header: "Risk",
      width: "2fr",
      render: (server) => statusColumn(server),
    },
    {
      key: "usage",
      header: "Usage",
      width: "1fr",
      render: (server) => <ShadowMCPInventoryUsageCell server={server} />,
    },
  ];

  return (
    <div className={compact ? "space-y-4" : "space-y-6"}>
      <MetricCard.Group className="flex-wrap">
        <MetricCard
          label="Tracked servers"
          value={inventory.length}
          tone="information"
          size="xs"
        />
        <MetricCard
          label="Pending requests"
          value={totalPendingRequests}
          tone={totalPendingRequests > 0 ? "destructive" : "neutral"}
          size="xs"
        />
        <MetricCard
          label="Blocked or restricted"
          value={restrictedOrBlockedCount}
          tone={restrictedOrBlockedCount > 0 ? "warning" : "neutral"}
          size="xs"
        />
      </MetricCard.Group>
      <Card.Dashboard
        title="Top risky Shadow MCP servers"
        action={
          action ? (
            <Button size="sm" variant="secondary" onClick={action.onClick}>
              <Button.Text>{action.label}</Button.Text>
            </Button>
          ) : (
            <Button
              size="sm"
              variant="secondary"
              onClick={() =>
                void navigate(`${routes.shadowMCP.href()}?tab=inventory`)
              }
            >
              <Button.Text>Open full inventory</Button.Text>
            </Button>
          )
        }
      >
        {inventoryQuery.isPending ? (
          <div
            aria-label="Loading risky Shadow MCP servers"
            className="flex flex-col gap-4"
            role="status"
          >
            <SkeletonTable />
          </div>
        ) : inventoryQuery.isError ? (
          <InlineEmptyState
            icon="triangle-alert"
            heading="Shadow MCP inventory couldn't be loaded"
            description="Retry to rank risky servers by review and enforcement state."
            action={
              <Button size="sm" onClick={() => void inventoryQuery.refetch()}>
                <Button.Text>Retry</Button.Text>
              </Button>
            }
          />
        ) : riskyServers.length === 0 ? (
          <InlineEmptyState
            icon="shield-check"
            heading="No risky servers right now"
            description="Shadow MCP has no pending, blocked, or restricted servers in this project."
          />
        ) : (
          <Table
            columns={columns}
            data={riskyServers}
            rowKey={(server) => server.serverSlug}
            onRowClick={(server) =>
              routes.shadowMCP.detail.goTo(server.serverSlug)
            }
          />
        )}
      </Card.Dashboard>
    </div>
  );
}
