import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { Heading } from "@/components/ui/Heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { dateTimeFormatters } from "@/lib/dates";
import { isNotFoundError, isUuidRouteParam } from "@/lib/route-errors";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  useDeployment,
  useDeploymentSuspense,
} from "@gram/client/react-query/deployment.js";
import { Button } from "@/components/ui/Button";
import { MetricCard } from "@/components/ui/MetricCard";
import { Skeleton } from "@/components/ui/Skeleton";
import { RefreshCcwIcon } from "lucide-react";
import { Suspense } from "react";
import { useParams } from "react-router";
import { useActiveDeployment } from "../useActiveDeployment";
import { useRedeployDeployment } from "../useRedeployDeployment";
import { useFailedDeploymentSources } from "@/components/sources/useFailedDeploymentSources";
import { invalidateAllDeployment } from "@gram/client/react-query/deployment.js";
import { useQueryClient } from "@tanstack/react-query";
import { AssetsTabContent } from "./AssetsTabContent";
import { FailedSourcesSection } from "./FailedSourcesSection";
import { LogsTabContent } from "./LogsTabContent";
import { ToolsTabContent } from "./ToolsTabContent";
import {
  DeploymentPageSearchParams,
  useDeploymentSearchParams,
} from "./use-deployment-search-params";

export default function DeploymentPage(): JSX.Element {
  const { deploymentId } = useParams();

  if (!isUuidRouteParam(deploymentId)) {
    return <DeploymentRouteNotFound />;
  }

  return <DeploymentPageContent deploymentId={deploymentId} />;
}

function DeploymentPageContent({ deploymentId }: { deploymentId: string }) {
  const {
    data: deployment,
    error,
    isLoading,
  } = useDeployment({ id: deploymentId }, undefined, {
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    throwOnError: false,
  });

  if (isNotFoundError(error)) {
    return <DeploymentRouteNotFound />;
  }

  if (error) {
    throw error;
  }

  if (isLoading || !deployment) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Skeleton>
            <div className="h-4 w-1/3" />
          </Skeleton>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Suspense
          fallback={
            <Skeleton>
              <div className="h-4 w-1/3" />
            </Skeleton>
          }
        >
          <DeploymentLogs deploymentId={deploymentId} />
        </Suspense>
      </Page.Body>
    </Page>
  );
}

function DeploymentRouteNotFound() {
  const routes = useRoutes();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RouteNotFoundState
          title="Deployment not found"
          description="This deployment may have been deleted, or the link may be incomplete."
          action={
            <routes.deployments.Link>
              <SecondaryRouteAction>Back to deployments</SecondaryRouteAction>
            </routes.deployments.Link>
          }
        />
      </Page.Body>
    </Page>
  );
}

function DeploymentLogs(props: { deploymentId: string }) {
  const { deploymentId } = props;
  const queryClient = useQueryClient();

  const { searchParams, setSearchParams } = useDeploymentSearchParams();
  const failedDeployment = useFailedDeploymentSources(deploymentId);

  const handleUpdateTab = (tab: string) => {
    setSearchParams({ tab: tab as DeploymentPageSearchParams["tab"] });
  };

  return (
    <div className="grid w-full gap-16 overflow-x-hidden">
      <section className="min-w-0 space-y-6">
        <HeadingSection />

        <Suspense
          fallback={
            <Skeleton>
              <div className="h-4 w-1/3" />
            </Skeleton>
          }
        >
          <StatsSection
            onClickTools={() => setSearchParams({ tab: "tools" })}
            onClickAssets={() => setSearchParams({ tab: "assets" })}
          />
        </Suspense>

        {failedDeployment.hasFailures && failedDeployment.deployment && (
          <FailedSourcesSection
            failedSources={failedDeployment.failedSources}
            generalErrors={failedDeployment.generalErrors}
            deployment={failedDeployment.deployment}
            onRemoveSuccess={() => {
              void invalidateAllDeployment(queryClient);
            }}
          />
        )}
      </section>

      <Tabs
        value={searchParams.tab}
        onValueChange={handleUpdateTab}
        className="min-w-0 gap-16"
      >
        <TabsList>
          <TabsTrigger value="logs">Logs</TabsTrigger>
          <TabsTrigger value="assets">Assets</TabsTrigger>
          <TabsTrigger value="tools">Tools</TabsTrigger>
        </TabsList>
        <TabsContent value="logs">
          <Suspense
            fallback={
              <Skeleton>
                <div className="h-4 w-1/3" />
              </Skeleton>
            }
          >
            <LogsTabContent />
          </Suspense>
        </TabsContent>
        <TabsContent value="assets">
          <Suspense
            fallback={
              <Skeleton>
                <div className="h-4 w-1/3" />
              </Skeleton>
            }
          >
            <AssetsTabContent />
          </Suspense>
        </TabsContent>
        <TabsContent value="tools">
          <ToolsTabContent deploymentId={deploymentId} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

const HeadingSection = () => {
  const { deploymentId } = useParams();
  const { data: deployment } = useDeployment({ id: deploymentId! }, undefined, {
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  const { data: activeDeployment } = useActiveDeployment();
  const redeployMutation = useRedeployDeployment();

  const handleRedeploy = () => {
    redeployMutation.mutate({
      request: {
        redeployRequestBody: {
          deploymentId: deploymentId!,
        },
      },
    });
  };

  const RedeployButton = () => {
    if (!deployment)
      return (
        <Button onClick={handleRedeploy} disabled>
          <Button.LeftIcon>
            <RefreshCcwIcon size={16} />
          </Button.LeftIcon>
          <Button.Text>Roll Back</Button.Text>
        </Button>
      );

    const isActiveDeployment = activeDeployment?.id === deploymentId!;
    const { isPending } = redeployMutation;

    let buttonText: string;
    if (isActiveDeployment) {
      if (isPending) buttonText = "Retrying Deployment";
      else buttonText = "Retry Deployment";
    } else if (deployment.status === "completed") {
      if (isPending) buttonText = "Rolling Back...";
      else buttonText = "Roll Back";
    } else {
      if (isPending) buttonText = "Redeploying...";
      else buttonText = "Redeploy";
    }

    return (
      <Button onClick={handleRedeploy} disabled={isPending}>
        <Button.LeftIcon>
          <RefreshCcwIcon
            size={16}
            className={cn(isPending && "direction-reverse animate-spin")}
          />
        </Button.LeftIcon>
        <Button.Text>{buttonText}</Button.Text>
      </Button>
    );
  };

  return (
    <div className="flex items-center justify-between">
      <div className="flex flex-col gap-2">
        <Page.Eyebrow />
        <Heading variant="h1">Deployment Overview</Heading>
      </div>
      <RequireScope scope="project:write" level="section">
        <RedeployButton />
      </RequireScope>
    </div>
  );
};

type HeaderSectionStatsProps = {
  onClickAssets?: () => void;
  onClickTools?: () => void;
};

const StatsSection = ({
  onClickAssets,
  onClickTools,
}: HeaderSectionStatsProps) => {
  const { deploymentId } = useParams();
  const { data: deployment } = useDeploymentSuspense(
    { id: deploymentId! },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  );

  const humanizedDate = humanizeDeploymentDate(deployment.createdAt);
  let assetCount = deployment.openapiv3Assets.length;
  if (deployment.functionsAssets) {
    assetCount += deployment.functionsAssets.length;
  }
  assetCount += deployment.externalMcps?.length ?? 0;

  const toolCount =
    deployment.openapiv3ToolCount +
    deployment.functionsToolCount +
    deployment.externalMcpToolCount;

  const statusTone =
    deployment.status === "completed"
      ? "success"
      : deployment.status === "failed"
        ? "destructive"
        : "neutral";
  const statusWord =
    deployment.status === "completed"
      ? "Succeeded"
      : deployment.status === "failed"
        ? "Failed"
        : deployment.status;

  return (
    <div className="space-y-2">
      <div className="text-muted-foreground font-mono text-xs">
        {deployment.id}
      </div>
      <MetricCard.Group>
        <MetricCard
          label="Assets"
          value={assetCount}
          tone="information"
          size="sm"
          role="button"
          tabIndex={0}
          className="cursor-pointer"
          onClick={() => onClickAssets?.()}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              onClickAssets?.();
            }
          }}
        />
        <MetricCard
          label="Tools"
          value={toolCount}
          tone="information"
          size="sm"
          role="button"
          tabIndex={0}
          className="cursor-pointer"
          onClick={() => onClickTools?.()}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              onClickTools?.();
            }
          }}
        />
        <MetricCard
          label="Status"
          tone={statusTone}
          size="sm"
          value={
            <span className="font-mono text-lg tracking-wide uppercase">
              {statusWord}
            </span>
          }
        />
        <MetricCard
          label="Created"
          tone="neutral"
          size="sm"
          value={<span className="text-2xl">{humanizedDate}</span>}
        />
      </MetricCard.Group>
    </div>
  );
};

function humanizeDeploymentDate(date: Date) {
  const isOneDayOld = Date.now() - date.getTime() >= 24 * 60 * 60 * 1000;

  if (isOneDayOld) {
    return dateTimeFormatters.sameYear.format(date);
  }

  return dateTimeFormatters.humanize(date);
}
