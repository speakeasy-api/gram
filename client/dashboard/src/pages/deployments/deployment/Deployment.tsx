import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import {
  useDeployment,
  useDeploymentLogs,
  useDeploymentSuspense,
} from "@gram/client/react-query";
import { Button, Separator, Skeleton } from "@speakeasy-api/moonshine";
import {
  CheckIcon,
  DotIcon,
  FileCodeIcon,
  RefreshCcwIcon,
  WrenchIcon,
  XIcon,
} from "lucide-react";
import { memo, Suspense } from "react";
import { useParams } from "react-router";
import { useActiveDeployment } from "../useActiveDeployment";
import { useRedeployDeployment } from "../useRedeployDeployment";
import { AssetsTabContent } from "./AssetsTabContent";
import { LogsTabContent } from "./LogsTabContent";
import { ToolsTabContent } from "./ToolsTabContent";
import {
  DeploymentPageSearchParams,
  useDeploymentSearchParams,
} from "./use-deployment-search-params";

export default function DeploymentPage() {
  const { deploymentId } = useParams();
  if (!deploymentId) {
    return <p className="text-destructive">Error: Deployment ID is required</p>;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Suspense fallback={<div>Loading logs...</div>}>
          <DeploymentLogs deploymentId={deploymentId} />
        </Suspense>
      </Page.Body>
    </Page>
  );
}

function DeploymentLogs(props: { deploymentId: string }) {
  const { deploymentId } = props;

  const { searchParams, setSearchParams } = useDeploymentSearchParams();

  const handleUpdateTab = (tab: string) => {
    setSearchParams({ tab: tab as DeploymentPageSearchParams["tab"] });
  };

  return (
    <div className="grid gap-16 w-full overflow-x-hidden">
      <section className="space-y-6 min-w-0">
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
      </section>

      <Tabs
        value={searchParams.tab}
        onValueChange={handleUpdateTab}
        className="gap-16 min-w-0"
      >
        <TabsList>
          <TabsTrigger value="logs">Logs</TabsTrigger>
          <TabsTrigger value="assets">Assets</TabsTrigger>
          <TabsTrigger value="tools">Tools</TabsTrigger>
        </TabsList>
        <TabsContent value="logs">
          <Suspense fallback={<div>Loading logs...</div>}>
            <LogsTabContent />
          </Suspense>
        </TabsContent>
        <TabsContent value="assets">
          <Suspense fallback={<div>Loading assets...</div>}>
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
    } else return null;

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
      <Heading variant="h1">Deployment Overview</Heading>
      <RedeployButton />
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

  return (
    <div className="text-sm flex items-center gap-3 h-4">
      <span>{deployment.id}</span>
      <Separator orientation="vertical" />
      <div className="flex items-center gap-0.5">
        <HumanizedDeploymentStatus status={deployment.status} />
        <DotIcon className="text-border" />
        {humanizedDate}
      </div>
      <Separator orientation="vertical" />
      <button
        className="flex items-center gap-1"
        onClick={() => onClickAssets?.()}
      >
        <FileCodeIcon size={16} />
        {assetCount} Assets
      </button>
      <Separator orientation="vertical" />
      <button
        className="flex items-center gap-1"
        onClick={() => onClickTools?.()}
      >
        <WrenchIcon size={16} />
        {deployment.openapiv3ToolCount +
          deployment.functionsToolCount +
          deployment.externalMcpToolCount}{" "}
        Tools
      </button>
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

const HumanizedDeploymentStatus = memo((props: { status: string }) => {
  if (props.status === "completed") {
    return (
      <div className="flex items-center">
        <CheckIcon className="size-4 text-default-success" />
        <span className="ml-2">Succeeded</span>
      </div>
    );
  }

  if (props.status === "failed") {
    return (
      <div className="flex items-center">
        <XIcon className="size-4 text-destructive" />
        <span className="ml-2">Failed</span>
      </div>
    );
  }

  return <span className="capitalize">{props.status}</span>;
});

export function useDeploymentLogsSummary(deploymentId: string | undefined) {
  const { data: logs } = useDeploymentLogs(
    { deploymentId: deploymentId! },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
      enabled: !!deploymentId,
    },
  );

  return logs?.events.reduce(
    (acc, event) => {
      if (event.message.includes("skipped")) {
        acc.skipped++;
      }
      // Skipped are also errors
      if (event.event.includes("error")) {
        acc.errors++;
      }
      return acc;
    },
    { skipped: 0, errors: 0 },
  );
}
