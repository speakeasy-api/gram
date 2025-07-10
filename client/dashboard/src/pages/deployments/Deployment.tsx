import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { cn } from "@/lib/utils";
import { useDeploymentLogsSuspense } from "@gram/client/react-query";
import { Suspense } from "react";
import { useParams } from "react-router";

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
  const { data: res } = useDeploymentLogsSuspense({ deploymentId }, undefined, {
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  return (
    <>
      <Heading variant="h2">Logs</Heading>
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
}
