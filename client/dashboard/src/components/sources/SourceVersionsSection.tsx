import { DeploymentsEmptyState } from "@/pages/deployments/DeploymentsEmptyState";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useActiveDeployment } from "@/hooks/toolTypes";
import { dateTimeFormatters } from "@/lib/dates";
import { useRoutes } from "@/routes";
import type { DeploymentSummary } from "@gram/client/models/components/deploymentsummary.js";
import { useListDeployments } from "@gram/client/react-query/listDeployments.js";
import { ExternalLink } from "lucide-react";
import { SourceSectionError } from "./SourceSectionError";
import { deploymentStatusBadge } from "./sourceVersions";

const VERSION_LIMIT = 10;

function VersionRow({
  deployment,
  isActive,
}: {
  deployment: DeploymentSummary;
  isActive: boolean;
}): JSX.Element {
  const routes = useRoutes();
  const badge = deploymentStatusBadge(deployment.status, isActive);
  const sourceCount =
    deployment.openapiv3AssetCount + deployment.functionsAssetCount;

  return (
    <li className="flex items-center justify-between gap-4 px-6 py-3">
      <div className="flex min-w-0 items-center gap-3">
        <routes.deployments.deployment.Link
          params={[deployment.id]}
          className="truncate font-mono text-xs"
        >
          {deployment.id.slice(0, 8)}
        </routes.deployments.deployment.Link>
        <Badge variant={badge.variant} size="sm">
          <Badge.Text>{badge.label}</Badge.Text>
        </Badge>
      </div>
      <div className="flex shrink-0 items-center gap-4">
        <Text muted className="text-xs">
          {sourceCount} {sourceCount === 1 ? "source" : "sources"}
        </Text>
        <Text muted className="text-xs">
          {dateTimeFormatters.humanize(deployment.createdAt, {
            includeTime: false,
          })}
        </Text>
      </div>
    </li>
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

/**
 * The deployments a source is versioned by.
 *
 * Sources are not versioned individually: a push deploys every source in the
 * project together, so a source's history is the project's deployments. Each
 * row says what state a version is in and links to the deployment page,
 * which holds the logs and the full asset list.
 */
export function SourceVersionsSection(): JSX.Element {
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
        isEmpty={versions.length === 0}
      >
        <ol className="divide-border divide-y">
          {versions.map((version) => (
            <VersionRow
              key={version.id}
              deployment={version}
              isActive={version.id === activeId}
            />
          ))}
        </ol>
      </SourceVersionsBody>
    </Card.Dashboard>
  );
}
