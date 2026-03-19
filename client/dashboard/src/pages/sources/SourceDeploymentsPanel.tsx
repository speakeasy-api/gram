import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useDeploymentsForSourceSuspense } from "@gram/client/react-query/deploymentsForSource.js";
import type { SourceDeploymentSummary } from "@gram/client/models/components";
import { Kind } from "@gram/client/models/operations/deploymentsforsource.js";
import { useRoutes } from "@/routes";
import { useRedeploySource } from "@/components/sources/useRedeploySource";
import { Badge, Button, Icon } from "@speakeasy-api/moonshine";
import { ExternalLink } from "lucide-react";
import { Suspense, useState } from "react";
import { useParams } from "react-router";
import { DeploymentsEmptyState } from "../deployments/DeploymentsEmptyState";
import { useActiveDeployment } from "@gram/client/react-query/index.js";
import { LogsTabContent } from "../deployments/deployment/LogsTabContent";

// ─── Labeled badge ───────────────────────────────────────────

function LabeledBadge({ label, value }: { label: string; value: string }) {
  return (
    <Badge variant="neutral" className="p-0 gap-0 text-[10px]">
      <span className="bg-foreground/10 text-muted-foreground pl-1.5 pr-1 py-0.5 rounded-l-[inherit]">
        {label}
      </span>
      <span className="bg-foreground/10 w-1.5 self-stretch [clip-path:polygon(0_0,100%_50%,0_100%)]" />
      <span className="pl-1.5 pr-1.5 py-0.5 font-mono">{value}</span>
    </Badge>
  );
}

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
  deployment: SourceDeploymentSummary;
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
        "w-full text-left px-3 py-4 border-b border-border transition-colors",
        "hover:bg-muted/50",
        isSelected && "bg-muted",
      )}
    >
      <div className="flex items-center gap-2">
        <StatusDot status={deployment.status} />
        <LabeledBadge label="Version" value={deployment.assetId.slice(-8)} />
        {isActive && (
          <Badge variant="success" className="py-0 px-1.5 ml-auto">
            <Badge.Text className="text-[10px]">Active</Badge.Text>
          </Badge>
        )}
      </div>
      <div className="flex items-center gap-2 mt-2 pl-4">
        <span className="text-xs text-muted-foreground">{timeLabel}</span>
      </div>
    </button>
  );
}

// ─── Detail panel ────────────────────────────────────────────

function DeploymentDetailPanel({
  deployment,
  isActive,
  activeAssetId,
  sourceSlug,
  sourceType,
  attachmentType,
}: {
  deployment: SourceDeploymentSummary;
  isActive: boolean;
  activeAssetId?: string;
  sourceSlug?: string;
  sourceType: "openapi" | "function" | "externalmcp";
  attachmentType?: string;
}) {
  const redeployMutation = useRedeploySource();

  const isCurrentVersion =
    activeAssetId != null && deployment.assetId === activeAssetId;
  const canRedeploy =
    !isActive &&
    deployment.status === "completed" &&
    sourceSlug &&
    !isCurrentVersion;

  return (
    <div className="flex-1 overflow-y-auto p-6 space-y-6">
      {/* Info section */}
      <div className="rounded-lg border border-border p-4">
        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
          <div>
            <dt className="text-muted-foreground text-xs mb-0.5">
              Source Version
            </dt>
            <dd className="flex items-center gap-1">
              <span className="font-mono">{deployment.assetId.slice(-8)}</span>
              <CopyButton text={deployment.assetId} size="inline" />
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
            <dd>{dateTimeFormatters.humanize(deployment.createdAt)}</dd>
          </div>

          <div>
            <dt className="text-muted-foreground text-xs mb-0.5">Tools</dt>
            <dd>{deployment.toolCount}</dd>
          </div>
        </dl>
      </div>

      {canRedeploy ? (
        <Button
          variant="secondary"
          size="sm"
          disabled={redeployMutation.isPending}
          onClick={() =>
            redeployMutation.mutate({
              deploymentId: deployment.id,
              slug: sourceSlug,
              type: sourceType,
            })
          }
        >
          <Button.LeftIcon>
            <Icon name="refresh-cw" className="size-4" />
          </Button.LeftIcon>
          <Button.Text>
            {redeployMutation.isPending ? "Rolling back..." : "Rollback"}
          </Button.Text>
        </Button>
      ) : isCurrentVersion && !isActive ? (
        <Badge variant="success" className="py-0.5 px-2">
          <Badge.Text className="text-xs">Current version</Badge.Text>
        </Badge>
      ) : null}

      {/* Logs section */}
      <Suspense
        fallback={
          <div className="text-sm text-muted-foreground">Loading logs...</div>
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

function sourceKindToApiKind(
  sourceKind?: string,
): "openapi" | "function" | "externalmcp" {
  if (sourceKind === "function") return "function";
  if (sourceKind === "externalmcp") return "externalmcp";
  return "openapi";
}

function sourceKindToSdkKind(sourceKind?: string): Kind {
  if (sourceKind === "function") return Kind.Function;
  if (sourceKind === "externalmcp") return Kind.Externalmcp;
  return Kind.Openapi;
}

export function SourceDeploymentsPanel({
  sourceKind,
  attachmentType,
}: {
  sourceKind?: string;
  attachmentType?: string;
}) {
  const { sourceSlug } = useParams<{ sourceSlug: string }>();
  const { data: res } = useDeploymentsForSourceSuspense({
    slug: sourceSlug ?? "",
    kind: sourceKindToSdkKind(sourceKind),
  });
  const deployments = res.items ?? [];
  const { data: activeDeploymentResult } = useActiveDeployment();
  const activeDeployment = activeDeploymentResult?.deployment;
  const routes = useRoutes();

  const sourceType = sourceKindToApiKind(sourceKind);

  // Find the active deployment's asset ID from the list
  const activeItem = deployments.find((d) => d.id === activeDeployment?.id);

  const [selectedId, setSelectedId] = useState<string | null>(
    deployments[0]?.id ?? null,
  );

  if (deployments.length === 0) {
    return <DeploymentsEmptyState />;
  }

  const selectedDeployment =
    deployments.find((d) => d.id === selectedId) ?? deployments[0]!;

  return (
    <div className="max-w-[1270px] mx-auto px-8 py-8 w-full h-full min-h-0 flex flex-col">
      <div className="flex items-center justify-between mb-4 shrink-0">
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

      <div className="grid grid-cols-[280px_1fr] flex-1 min-h-0 rounded-lg border border-border overflow-hidden">
        {/* ── Left sidebar ── */}
        <div className="border-r border-border overflow-y-auto bg-muted/30">
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
          activeAssetId={activeItem?.assetId}
          sourceSlug={sourceSlug}
          sourceType={sourceType}
          attachmentType={attachmentType}
        />
      </div>
    </div>
  );
}
