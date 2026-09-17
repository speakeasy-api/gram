import { DeploymentsEmptyState } from "@/pages/deployments/DeploymentsEmptyState";
import { LogsTabContent } from "@/pages/deployments/deployment/LogsTabContent";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { CopyButton } from "@/components/ui/CopyButton";
import { Skeleton, SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useActiveDeployment } from "@/hooks/toolTypes";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { DeploymentSummary } from "@gram/client/models/components/deploymentsummary.js";
import { useListDeployments } from "@gram/client/react-query/listDeployments.js";
import { ExternalLink } from "lucide-react";
import { Suspense, useState } from "react";
import {
  attachmentTypeForSourceKind,
  deploymentCountsForSourceKind,
  deploymentStatusBadge,
  type SourceKind,
} from "./sourceVersions";

const VERSION_LIMIT = 10;

function DeploymentStatusBadge({
  status,
  isActive,
}: {
  status: string;
  isActive: boolean;
}): JSX.Element {
  const badge = deploymentStatusBadge(status, isActive);
  return (
    <Badge variant={badge.variant} size="sm">
      <Badge.Text>{badge.label}</Badge.Text>
    </Badge>
  );
}

function VersionListItem({
  deployment,
  isActive,
  isSelected,
  onSelect,
}: {
  deployment: DeploymentSummary;
  isActive: boolean;
  isSelected: boolean;
  onSelect: () => void;
}): JSX.Element {
  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        aria-current={isSelected ? "true" : undefined}
        className={cn(
          "hover:bg-muted/50 flex w-full flex-col gap-1 border-b px-4 py-3 text-left transition-colors",
          isSelected && "bg-muted",
        )}
      >
        <div className="flex items-center justify-between gap-2">
          <span className="font-mono text-xs">{deployment.id.slice(0, 8)}</span>
          <DeploymentStatusBadge
            status={deployment.status}
            isActive={isActive}
          />
        </div>
        <Text muted className="text-xs">
          {dateTimeFormatters.humanize(deployment.createdAt, {
            includeTime: false,
          })}
        </Text>
      </button>
    </li>
  );
}

function VersionFact({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div>
      <dt className="text-eyebrow mb-1">{label}</dt>
      <dd className="flex items-center gap-2 text-sm">{children}</dd>
    </div>
  );
}

function VersionDetail({
  deployment,
  isActive,
  sourceKind,
}: {
  deployment: DeploymentSummary;
  isActive: boolean;
  sourceKind: SourceKind;
}): JSX.Element {
  const routes = useRoutes();
  const counts = deploymentCountsForSourceKind(sourceKind, deployment);

  return (
    <div className="flex min-w-0 flex-col gap-6 p-6">
      <dl className="grid grid-cols-2 gap-x-6 gap-y-4 border p-4 sm:grid-cols-4">
        <VersionFact label="Deployment">
          <routes.deployments.deployment.Link
            params={[deployment.id]}
            className="font-mono"
          >
            {deployment.id.slice(0, 8)}
          </routes.deployments.deployment.Link>
          <CopyButton text={deployment.id} size="xs" />
        </VersionFact>
        <VersionFact label="Status">
          <DeploymentStatusBadge
            status={deployment.status}
            isActive={isActive}
          />
        </VersionFact>
        <VersionFact label="Created">
          {dateTimeFormatters.humanize(deployment.createdAt)}
        </VersionFact>
        <VersionFact label={`${counts.assetLabel} / tools`}>
          {counts.assetCount} / {counts.toolCount}
        </VersionFact>
      </dl>
      {/* The logs for this kind of source only: a function's build output
          is noise on an OpenAPI document's page, and the reverse. */}
      <Suspense fallback={<Skeleton className="h-40" />}>
        <LogsTabContent
          deploymentId={deployment.id}
          embeddedMode
          attachmentType={attachmentTypeForSourceKind(sourceKind)}
        />
      </Suspense>
    </div>
  );
}

/**
 * The deployments a source is versioned by, with each one's logs for this
 * kind of source.
 *
 * Sources are not versioned individually: a push deploys every source in the
 * project together, so a source's history is the project's deployments.
 */
export function SourceVersionsSection({
  sourceKind,
}: {
  sourceKind: SourceKind;
}): JSX.Element {
  const routes = useRoutes();
  const { data, isLoading } = useListDeployments({}, {});
  const { data: activeResult } = useActiveDeployment();
  const activeId = activeResult?.deployment?.id;
  const versions = (data?.items ?? []).slice(0, VERSION_LIMIT);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // The newest version is the default until one is picked; a list that
  // refetches with a new head keeps the pick rather than jumping.
  const selected =
    versions.find((version) => version.id === selectedId) ?? versions[0];

  return (
    <Card.Dashboard
      title="Versions"
      tooltip="Each push deploys every source in the project together, so a source's versions are the project's deployments."
      bodyClassName={versions.length === 0 ? undefined : "p-0"}
      action={
        <Button variant="tertiary" size="sm" asChild>
          <routes.mcp.deployments.Link>
            <Button.Text>View all</Button.Text>
            <Button.RightIcon>
              <ExternalLink className="size-3" />
            </Button.RightIcon>
          </routes.mcp.deployments.Link>
        </Button>
      }
    >
      <SourceVersionsBody isLoading={isLoading} isEmpty={!selected}>
        {selected && (
          <div className="grid min-h-0 grid-cols-[240px_minmax(0,1fr)]">
            <ol className="bg-muted/30 max-h-[640px] overflow-y-auto border-r">
              {versions.map((version) => (
                <VersionListItem
                  key={version.id}
                  deployment={version}
                  isActive={version.id === activeId}
                  isSelected={version.id === selected.id}
                  onSelect={() => setSelectedId(version.id)}
                />
              ))}
            </ol>
            <VersionDetail
              key={selected.id}
              deployment={selected}
              isActive={selected.id === activeId}
              sourceKind={sourceKind}
            />
          </div>
        )}
      </SourceVersionsBody>
    </Card.Dashboard>
  );
}

function SourceVersionsBody({
  isLoading,
  isEmpty,
  children,
}: {
  isLoading: boolean;
  isEmpty: boolean;
  children: React.ReactNode;
}): JSX.Element {
  if (isLoading) {
    return (
      <div className="p-6">
        <SkeletonTable />
      </div>
    );
  }
  if (isEmpty) return <DeploymentsEmptyState />;
  return <>{children}</>;
}
