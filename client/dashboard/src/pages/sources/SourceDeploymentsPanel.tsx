import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useListDeploymentsSuspense } from "@gram/client/react-query/index.js";
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
        "size-2 shrink-0 rounded-full",
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
        "border-border w-full border-b px-3 py-3 text-left transition-colors",
        "hover:bg-muted/50",
        isSelected && "bg-muted",
      )}
    >
      <div className="flex items-center gap-2">
        <StatusDot status={deployment.status} />
        <span className="text-sm capitalize">{deployment.status}</span>
        <span className="ml-auto">
          {isActive ? (
            <Badge variant="success" className="px-1.5 py-0">
              <Badge.Text className="text-[10px]">Active</Badge.Text>
            </Badge>
          ) : deployment.status === "completed" ? (
            <Badge variant="neutral" className="px-1.5 py-0">
              <Badge.Text className="text-[10px]">Completed</Badge.Text>
            </Badge>
          ) : deployment.status === "failed" ? (
            <Badge variant="destructive" className="px-1.5 py-0">
              <Badge.Text className="text-[10px]">Failed</Badge.Text>
            </Badge>
          ) : (
            <Badge variant="warning" className="px-1.5 py-0">
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
      <div className="mt-1 flex items-center gap-2 pl-4">
        <span className="text-muted-foreground font-mono text-xs">
          {deployment.id.slice(0, 8)}
        </span>
        <span className="text-muted-foreground text-xs">&middot;</span>
        <span className="text-muted-foreground text-xs">{timeLabel}</span>
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
    <div className="flex-1 space-y-6 overflow-y-auto p-6">
      {/* Info section */}
      <div className="border-border rounded-lg border p-4">
        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
          <div>
            <dt className="text-muted-foreground mb-0.5 text-xs">
              Deployment ID
            </dt>
            <dd className="flex items-center gap-1">
              <span className="font-mono">{deployment.id.slice(0, 8)}</span>
              <CopyButton text={deployment.id} size="inline" />
            </dd>
          </div>

          <div>
            <dt className="text-muted-foreground mb-0.5 text-xs">Status</dt>
            <dd className="flex items-center gap-2">
              <StatusDot status={deployment.status} />
              <span className="capitalize">{deployment.status}</span>
              {isActive && (
                <Badge variant="success" className="px-1.5 py-0">
                  <Badge.Text className="text-[10px]">Active</Badge.Text>
                </Badge>
              )}
            </dd>
          </div>

          <div>
            <dt className="text-muted-foreground mb-0.5 text-xs">Created</dt>
            <dd>{dateTimeFormatters.humanize(deployment.createdAt)}</dd>
          </div>

          <div className="flex gap-6">
            <div>
              <dt className="text-muted-foreground mb-0.5 text-xs">
                {assetLabel}
              </dt>
              <dd>{assetCount}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground mb-0.5 text-xs">Tools</dt>
              <dd>{toolCount}</dd>
            </div>
          </div>
        </dl>
      </div>

      {/* Logs section */}
      <Suspense
        fallback={
          <div className="text-muted-foreground text-sm">Loading logs...</div>
        }
      >
        <LogsTabContent
          deploymentId={deployment.id}
          embeddedMode
          attachmentType={attachmentType}
        />
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
    <div className="mx-auto flex h-full min-h-0 w-full max-w-[1270px] flex-col px-8 py-8">
      <div className="mb-4 flex shrink-0 items-center justify-between">
        <div>
          <Heading variant="h4">Deployments</Heading>
          <Type muted small>
            {deployments.length} total
          </Type>
        </div>
        <routes.deployments.Link className="hover:no-underline">
          <Button variant="tertiary" size="sm">
            <Button.Text>View all</Button.Text>
            <Button.RightIcon>
              <ExternalLink className="size-3" />
            </Button.RightIcon>
          </Button>
        </routes.deployments.Link>
      </div>

      <div className="border-border grid min-h-0 flex-1 grid-cols-[280px_1fr] overflow-hidden rounded-lg border">
        {/* ── Left sidebar ── */}
        <div className="border-border bg-muted/30 overflow-y-auto border-r">
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
    </div>
  );
}
