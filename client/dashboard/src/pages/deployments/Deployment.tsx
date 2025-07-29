import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { HumanizeDateTime } from "@/lib/dates";
import { cn, getServerURL } from "@/lib/utils";
import {
  useDeploymentLogs,
  useDeploymentLogsSuspense,
  useDeploymentSuspense,
} from "@gram/client/react-query";
import { Icon } from "@speakeasy-api/moonshine";
import { Suspense } from "react";
import { useParams } from "react-router";
import { DeploymentLink } from "./Deployments";
import { ToolsList } from "./ToolsList";
import { Type } from "@/components/ui/type";

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
  const { data: deployment } = useDeploymentSuspense(
    { id: deploymentId },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    }
  );
  const { data: res } = useDeploymentLogsSuspense({ deploymentId }, undefined, {
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  return (
    <>
      <div className="mb-6">
        <Heading variant="h2" className="mb-4">
          Overview
        </Heading>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4">
          <dt><Type muted>Created</Type></dt>
          <dd>
            <HumanizeDateTime date={deployment.createdAt} />
          </dd>
          {deployment.clonedFrom ? (
            <>
              <dt><Type muted>Predecessor</Type></dt>
              <dd>
                <DeploymentLink id={deployment.clonedFrom} />
              </dd>
            </>
          ) : null}
        </dl>
      </div>
      <div className="mb-6">
        <Heading variant="h2" className="mb-4">
          OpenAPI Documents
        </Heading>
        <ul className="flex gap-2 flex-wrap">
          {deployment.openapiv3Assets.map((asset) => {
            const downloadURL = new URL(
              "/rpc/assets.serveOpenAPIv3",
              getServerURL()
            );
            downloadURL.searchParams.set("id", asset.assetId);

            return (
              <li
                key={asset.id}
                className="text-xl flex flex-nowrap gap-1 items-center bg-muted py-1 px-2 rounded-md"
              >
                <Icon
                  name="file-text"
                  size="small"
                  className="text-muted-foreground"
                />
                <a href={`${downloadURL}`} download>
                  <Type>{asset.name}</Type>
                </a>
              </li>
            );
          })}
        </ul>
      </div>
      <div className="mb-6">
        <Heading variant="h2" className="mb-4">
          Logs
        </Heading>
        <ol className="font-mono w-full overflow-auto bg-muted p-4 rounded space-y-1">
          {res.events.map((event, index) => {
            return (
              <li
                id={`event-${event.id}`}
                key={event.id}
                className={cn(
                  "whitespace-nowrap grid grid-cols-[max-content_1fr] gap-2 hover:not-target:bg-primary/10 target:bg-primary/30",
                  event.event.includes("error") ? "text-destructive" : ""
                )}
              >
                <a
                  href={`#event-${event.id}`}
                  className="text-muted-foreground"
                >
                  {index + 1}.
                </a>
                <pre>{event.message}</pre>
              </li>
            );
          })}
        </ol>
      </div>
      <div className="mb-8 pb-8">
        <Heading variant="h2" className="mb-4">
          Tools
        </Heading>
        <ToolsList deploymentId={deploymentId} />
      </div>
    </>
  );
}

export function useDeploymentLogsSummary(deploymentId: string | undefined) {
  const { data: logs } = useDeploymentLogs(
    { deploymentId: deploymentId! },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
      enabled: !!deploymentId,
    }
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
    { skipped: 0, errors: 0 }
  );
}
