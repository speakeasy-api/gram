import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { dateTimeFormatters } from "@/lib/dates";
import { cn, getServerURL } from "@/lib/utils";
import {
  DeploymentLogEvent,
  OpenAPIv3DeploymentAsset,
} from "@gram/client/models/components";
import {
  useDeploymentLogs,
  useDeploymentLogsSuspense,
  useDeploymentSuspense,
} from "@gram/client/react-query";
import { Button, Separator } from "@speakeasy-api/moonshine";
import {
  CheckIcon,
  DotIcon,
  FileCodeIcon,
  FileTextIcon,
  RefreshCcwIcon,
  WrenchIcon,
  XIcon,
} from "lucide-react";
import { Suspense, useMemo } from "react";
import { useParams } from "react-router";
import { ToolsList } from "./ToolsList";
import { useActiveDeployment } from "./useActiveDeployment";
import { useRedeployDeployment } from "./useRedeployDeployment";

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
  const project = useProject();
  const { data: deployment } = useDeploymentSuspense(
    { id: deploymentId },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  );

  const { data: deploymentLogs } = useDeploymentLogsSuspense(
    { deploymentId },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  );

  const { data: activeDeployment } = useActiveDeployment();
  const redeployMutation = useRedeployDeployment();

  const humanizedDate = useMemo(() => {
    const isOneDayOld =
      Date.now() - deployment.createdAt.getTime() >= 24 * 60 * 60 * 1000;

    if (isOneDayOld) {
      return dateTimeFormatters.sameYear.format(deployment.createdAt);
    }

    return dateTimeFormatters.humanize(deployment.createdAt);
  }, [deployment.createdAt]);

  const handleRedeploy = () => {
    redeployMutation.mutate({
      request: {
        redeployRequestBody: {
          deploymentId: deployment.id,
        },
      },
    });
  };

  const RedeployButton = () => {
    const isActiveDeployment = activeDeployment?.id === deployment.id;
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
    <>
      <div className="grid gap-4">
        <div className="flex items-center justify-between">
          <Heading variant="h1">Deployment Overview</Heading>
          <RedeployButton />
        </div>

        <div className="text-sm flex items-center gap-3 h-4">
          <span>{deployment.id}</span>
          <Separator orientation="vertical" />
          <div className="flex items-center gap-0.5">
            <HumanizedDeploymentStatus status={deployment.status} />
            <DotIcon className="text-border" />
            {humanizedDate}
          </div>
          <Separator orientation="vertical" />
          <span className="flex items-center gap-1">
            <FileCodeIcon size={16} />
            {deployment.openapiv3Assets.length} Assets
          </span>
          <Separator orientation="vertical" />
          <span className="flex items-center gap-1">
            <WrenchIcon size={16} />
            {deployment.openapiv3ToolCount} Tools
          </span>
        </div>
      </div>

      <Tabs defaultValue="logs">
        <TabsList className="mb-4">
          <TabsTrigger value="logs">Logs</TabsTrigger>
          <TabsTrigger value="assets">Assets</TabsTrigger>
          <TabsTrigger value="tools">Tools</TabsTrigger>
        </TabsList>
        <TabsContent value="logs">
          <LogsTabContents events={deploymentLogs.events} />
        </TabsContent>
        <TabsContent value="assets">
          <AssetsTabContents
            projectId={project.id}
            assets={deployment.openapiv3Assets}
          />
        </TabsContent>
        <TabsContent value="tools">
          <ToolsTabContents deploymentId={deploymentId} />
        </TabsContent>
      </Tabs>
    </>
  );
}

const LogsTabContents = ({ events }: { events: DeploymentLogEvent[] }) => {
  return (
    <>
      <Heading variant="h2" className="mb-4">
        Logs
      </Heading>
      <ol className="font-mono w-full overflow-auto bg-muted p-4 rounded space-y-1">
        {events.map((event, index) => {
          return (
            <li
              id={`event-${event.id}`}
              key={event.id}
              className={cn(
                "whitespace-nowrap grid grid-cols-[max-content_1fr] gap-2 hover:not-target:bg-primary/10 target:bg-primary/30",
                event.event.includes("error") ? "text-destructive" : "",
              )}
            >
              <a href={`#event-${event.id}`} className="text-muted-foreground">
                {index + 1}.
              </a>
              <pre>{event.message}</pre>
            </li>
          );
        })}
      </ol>
    </>
  );
};

const AssetsTabContents = ({
  projectId,
  assets,
}: {
  projectId: string;
  assets: OpenAPIv3DeploymentAsset[];
}) => {
  return (
    <>
      <Heading variant="h2" className="mb-4">
        OpenAPI Documents
      </Heading>
      <ul className="flex gap-2 flex-wrap">
        {assets.map((asset) => {
          const downloadURL = new URL(
            "/rpc/assets.serveOpenAPIv3",
            getServerURL(),
          );
          downloadURL.searchParams.set("id", asset.assetId);
          downloadURL.searchParams.set("project_id", projectId);

          return (
            <li
              key={asset.id}
              className="text-xl flex flex-nowrap gap-1 items-center bg-muted py-1 px-2 rounded-md"
            >
              <FileTextIcon size={16} className="text-muted-foreground" />
              <a href={`${downloadURL}`} download>
                <Type>{asset.name}</Type>
              </a>
            </li>
          );
        })}
      </ul>
    </>
  );
};

const ToolsTabContents = ({ deploymentId }: { deploymentId: string }) => {
  return (
    <>
      <Heading variant="h2" className="mb-4">
        Tools
      </Heading>
      <ToolsList deploymentId={deploymentId} />
    </>
  );
};

const HumanizedDeploymentStatus = (props: { status: string }) => {
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
};

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
