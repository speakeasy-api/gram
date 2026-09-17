import { DeploymentsEmptyState } from "@/pages/deployments/DeploymentsEmptyState";
import { LogsTabContent } from "@/pages/deployments/deployment/LogsTabContent";
import { Badge } from "@/components/ui/Badge";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { CopyButton } from "@/components/ui/CopyButton";
import { Skeleton, SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useActiveDeployment } from "@/hooks/toolTypes";
import { dateTimeFormatters } from "@/lib/dates";
import { handleError, toError } from "@/lib/errors";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { DeploymentSummary } from "@gram/client/models/components/deploymentsummary.js";
import { useListDeployments } from "@gram/client/react-query/listDeployments.js";
import { QueryErrorResetBoundary } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { Suspense, useState } from "react";
import { ErrorBoundary, type FallbackProps } from "react-error-boundary";
import { SourceSectionError } from "./SourceSectionError";
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

// The logs read with suspense queries, which throw on failure. Caught here,
// a failed read stays inside the Versions card rather than replacing the page.
function VersionLogsErrorFallback({
  error,
  resetErrorBoundary,
}: FallbackProps): JSX.Element {
  handleError(toError(error), { silent: true });
  return (
    <SourceSectionError
      heading="Couldn't load this deployment's logs"
      description="The rest of the version is unaffected."
      onRetry={resetErrorBoundary}
    />
  );
}

function VersionLogs({
  deploymentId,
  sourceKind,
}: {
  deploymentId: string;
  sourceKind: SourceKind;
}): JSX.Element {
  return (
    <QueryErrorResetBoundary>
      {({ reset }) => (
        <ErrorBoundary
          onReset={reset}
          fallbackRender={(props) => <VersionLogsErrorFallback {...props} />}
        >
          <Suspense fallback={<Skeleton className="h-40" />}>
            <LogsTabContent
              deploymentId={deploymentId}
              embeddedMode
              attachmentType={attachmentTypeForSourceKind(sourceKind)}
            />
          </Suspense>
        </ErrorBoundary>
      )}
    </QueryErrorResetBoundary>
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
      <VersionLogs deploymentId={deployment.id} sourceKind={sourceKind} />
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
  // The list is one card of the page: a failed read is shown here, not
  // thrown to the page's boundary.
  const { data, isLoading, isError, refetch } = useListDeployments(
    {},
    {},
    { throwOnError: false },
  );
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
      <SourceVersionsBody
        isLoading={isLoading}
        isError={isError}
        onRetry={() => void refetch()}
        isEmpty={!selected}
      >
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
  isError,
  onRetry,
  isEmpty,
  children,
}: {
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
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
  // A list that failed to load is not a project without deployments.
  if (isError && isEmpty) {
    return (
      <SourceSectionError
        heading="Couldn't load deployments"
        description="The project's deployments could not be fetched, so this source's versions are unknown."
        onRetry={onRetry}
      />
    );
  }
  if (isEmpty) return <DeploymentsEmptyState />;
  // A refetch that failed keeps the last list; say so rather than pass it
  // off as current.
  if (isError) {
    return (
      <div className="flex flex-col gap-4">
        <Alert variant="error" dismissible={false}>
          <span className="flex flex-wrap items-center gap-3">
            The deployments list could not be refreshed. Showing the last
            versions loaded.
            <Button variant="tertiary" size="sm" onClick={onRetry}>
              <Button.Text>Retry</Button.Text>
            </Button>
          </span>
        </Alert>
        {children}
      </div>
    );
  }
  return <>{children}</>;
}
