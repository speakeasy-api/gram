import { InlineEmptyState } from "@/components/inline-empty-state";
import { Card } from "@/components/ui/Card";
import { MetricCard } from "@/components/ui/MetricCard";
import { Button } from "@/components/ui/Button";
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
import { useShadowMCPPolicyInventory } from "./useShadowMCPPolicyInventory";
import {
  shadowMCPGatewayUseCaseMetrics,
  shadowMCPOpportunityServers,
} from "./shadowMCPUseCases";

const EMPTY_INVENTORY: ShadowMCPInventoryServer[] = [];

export function ShadowMCPGatewayUseCaseSection({
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
  const opportunityServers = useMemo(
    () => shadowMCPOpportunityServers(inventory, compact ? 5 : 8),
    [inventory, compact],
  );
  const { totalObservedCalls, concentratedUsageCount } = useMemo(
    () => shadowMCPGatewayUseCaseMetrics(inventory),
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
      key: "usage",
      header: "Usage",
      width: "1fr",
      render: (server) => <ShadowMCPInventoryUsageCell server={server} />,
    },
    {
      key: "leverage",
      header: "Calls / user",
      width: "1fr",
      render: (server) => (
        <Text variant="small">
          {(server.observedUseCount / Math.max(server.userCount, 1)).toFixed(1)}
        </Text>
      ),
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
          label="Observed calls"
          value={totalObservedCalls}
          tone="neutral"
          size="xs"
        />
        <MetricCard
          label="Concentrated usage"
          value={concentratedUsageCount}
          tone={concentratedUsageCount > 0 ? "warning" : "neutral"}
          size="xs"
          description="10+ calls from 3 users or fewer"
        />
      </MetricCard.Group>
      <Card.Dashboard
        title="Shadow MCP distribution opportunities"
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
            aria-label="Loading Shadow MCP distribution opportunities"
            className="flex flex-col gap-4"
            role="status"
          >
            <SkeletonTable />
          </div>
        ) : inventoryQuery.isError ? (
          <InlineEmptyState
            icon="triangle-alert"
            heading="Shadow MCP inventory couldn't be loaded"
            description="Retry to detect concentrated server usage for distribution."
            action={
              <Button size="sm" onClick={() => void inventoryQuery.refetch()}>
                <Button.Text>Retry</Button.Text>
              </Button>
            }
          />
        ) : opportunityServers.length === 0 ? (
          <InlineEmptyState
            icon="network"
            heading="No usage opportunities yet"
            description="Once users call external servers, Shadow MCP can highlight candidates to spread through gateways."
          />
        ) : (
          <Table
            columns={columns}
            data={opportunityServers}
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
