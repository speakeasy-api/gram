import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import {
  useListDeploymentsSuspense,
} from "@gram/client/react-query/index.js";
import type { DeploymentSummary } from "@gram/client/models/components";
import { useRoutes } from "@/routes";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { ExternalLink } from "lucide-react";
import { Suspense, useState } from "react";
import { DeploymentsEmptyState } from "../deployments/DeploymentsEmptyState";
import { useActiveDeployment } from "../deployments/useActiveDeployment";
import { LogsTabContent } from "../deployments/deployment/LogsTabContent";

// ─── Status dot ──────────────────────────────────────────────

function StatusDot({ status }: { status: string }) {
  return (
    <span
      className={cn(
        "size-2 rounded-full shrink-0",
        status === "completed" && "bg-success",
        status === "failed" && "bg-destructive",
        status !== "completed" && status !== "failed" && "bg-warning",
      )}
    />
  );
}

// ─── Sidebar item ────────────────────────────────────────────

function DeploymentSidebarItem({
  deployment,
  isActive,
  isSelected,
  onClick,
}: {
  deployment: DeploymentSummary;
  isActive: boolean;
  isSelected: boolean;
  onClick: () => void;
}) {
  const timeLabel = dateTimeFormatters.humanize(deployment.createdAt, {
    includeTime: false,
  });

  return (
    <button
      onClick={onClick}
      className={cn(
        "w-full text-left px-3 py-3 border-b border-border transition-colors",
        "hover:bg-muted/50",
        isSelected && "bg-muted",
      )}
    >
      <div className="flex items-center gap-2">
        <StatusDot status={deployment.status} />
        <span className="text-sm capitalize">{deployment.status}</span>
        <span className="ml-auto">
          {isActive ? (
            <Badge variant="success" className="py-0 px-1.5">
              <Badge.Text className="text-[10px]">Active</Badge.Text>
            </Badge>
          ) : deployment.status === "completed" ? (
            <Badge variant="neutral" className="py-0 px-1.5">
              <Badge.Text className="text-[10px]">Completed</Badge.Text>
            </Badge>
          ) : deployment.status === "failed" ? (
            <Badge variant="destructive" className="py-0 px-1.5">
              <Badge.Text className="text-[10px]">Failed</Badge.Text>
            </Badge>
          ) : (
            <Badge variant="warning" className="py-0 px-1.5">
              <Badge.Text className="text-[10px]">
                {deployment.status === "pending"
                  ? "Pending"
                  : deployment.status === "building"
                    ? "Building"
                    : deployment.status}
              </Badge.Text>
            </Badge>
          )}
        </span>
      </div>
      <div className="flex items-center gap-2 mt-1 pl-4">
        <span className="text-xs font-mono text-muted-foreground">
          {deployment.id.slice(0, 8)}
        </span>
        <span className="text-xs text-muted-foreground">&middot;</span>
        <span className="text-xs text-muted-foreground">{timeLabel}</span>
      </div>
    </button>
  );
}

// ─── Detail panel ────────────────────────────────────────────

function DeploymentDetailPanel({
  deployment,
  isActive,
  sourceKind,
  attachmentType,
}: {
  deployment: DeploymentSummary;
  isActive: boolean;
  sourceKind?: string;
  attachmentType?: string;
}) {
  // Show source-type-specific counts when viewing a source detail page
  const isFunction = sourceKind === "function";
  const assetCount = isFunction
    ? deployment.functionsAssetCount
    : sourceKind === "http" || sourceKind === "openapi"
      ? deployment.openapiv3AssetCount
      : deployment.openapiv3AssetCount + deployment.functionsAssetCount;
  const toolCount = isFunction
    ? deployment.functionsToolCount
    : sourceKind === "http" || sourceKind === "openapi"
      ? deployment.openapiv3ToolCount
      : deployment.openapiv3ToolCount + deployment.functionsToolCount;
  const assetLabel = isFunction
    ? "Functions"
    : sourceKind === "http" || sourceKind === "openapi"
      ? "APIs"
      : "Assets";

  return (
    <div className="flex-1 overflow-y-auto p-6 space-y-6">
      {/* Info section */}
      <div className="rounded-lg border border-border p-4">
        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
          <div>
            <dt className="text-muted-foreground text-xs mb-0.5">
              Deployment ID
            </dt>
            <dd className="flex items-center gap-1">
              <span className="font-mono">{deployment.id.slice(0, 8)}</span>
              <CopyButton text={deployment.id} size="inline" />
            </dd>
          </div>

          <div>
            <dt className="text-muted-foreground text-xs mb-0.5">Status</dt>
            <dd className="flex items-center gap-2">
              <StatusDot status={deployment.status} />
              <span className="capitalize">{deployment.status}</span>
              {isActive && (
                <Badge variant="success" className="py-0 px-1.5">
                  <Badge.Text className="text-[10px]">Active</Badge.Text>
                </Badge>
              )}
            </dd>
          </div>

          <div>
            <dt className="text-muted-foreground text-xs mb-0.5">Created</dt>
            <dd>
              {dateTimeFormatters.humanize(deployment.createdAt)}
            </dd>
          </div>

          <div className="flex gap-6">
            <div>
              <dt className="text-muted-foreground text-xs mb-0.5">
                {assetLabel}
              </dt>
              <dd>{assetCount}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground text-xs mb-0.5">Tools</dt>
              <dd>{toolCount}</dd>
            </div>
          </div>
        </dl>
      </div>

      {/* Logs section */}
      <Suspense
        fallback={
          <div className="text-sm text-muted-foreground">Loading logs...</div>
        }
      >
        <LogsTabContent deploymentId={deployment.id} embeddedMode attachmentType={attachmentType} />
      </Suspense>
    </div>
  );
}

// ─── Main orchestrator ───────────────────────────────────────

export function SourceDeploymentsPanel({
  sourceKind,
  attachmentType,
}: {
  sourceKind?: string;
  attachmentType?: string;
}) {
  const { data: res } = useListDeploymentsSuspense();
  const deployments = res.items ?? [];
  const { data: activeDeployment } = useActiveDeployment();
  const routes = useRoutes();

  const [selectedId, setSelectedId] = useState<string | null>(
    deployments[0]?.id ?? null,
  );

  if (deployments.length === 0) {
    return <DeploymentsEmptyState />;
  }

  const selectedDeployment =
    deployments.find((d) => d.id === selectedId) ?? deployments[0]!;

  return (
    <div className="grid grid-cols-[280px_1fr] h-full min-h-0">
      {/* ── Left sidebar ── */}
      <div className="border-r border-border overflow-y-auto">
        <div className="px-3 py-3 border-b border-border flex items-start justify-between">
          <div>
            <Heading variant="h4">Deployments</Heading>
            <Type muted small>
              {deployments.length} total
            </Type>
          </div>
          <routes.deployments.Link className="hover:no-underline">
            <Button variant="tertiary" size="sm">
              <Button.Text>All</Button.Text>
              <Button.RightIcon>
                <ExternalLink className="size-3" />
              </Button.RightIcon>
            </Button>
          </routes.deployments.Link>
        </div>

        {deployments.map((d) => (
          <DeploymentSidebarItem
            key={d.id}
            deployment={d}
            isActive={activeDeployment?.id === d.id}
            isSelected={selectedDeployment.id === d.id}
            onClick={() => setSelectedId(d.id)}
          />
        ))}
      </div>

      {/* ── Right detail panel ── */}
      <DeploymentDetailPanel
        key={selectedDeployment.id}
        deployment={selectedDeployment}
        isActive={activeDeployment?.id === selectedDeployment.id}
        sourceKind={sourceKind}
        attachmentType={attachmentType}
      />
    </div>
  );
}
