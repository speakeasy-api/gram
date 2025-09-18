import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { cn, getServerURL } from "@/lib/utils";
import {
  useDeploymentLogsSuspense,
  useDeploymentSuspense,
} from "@gram/client/react-query";
import { DownloadIcon, FileCodeIcon, FileTextIcon } from "lucide-react";
import { useParams } from "react-router";
import { ToolsList } from "./ToolsList";
import { MiniCard } from "@/components/ui/card-mini";

export const LogsTabContents = () => {
  const { deploymentId } = useParams();
  const { data: deploymentLogs } = useDeploymentLogsSuspense(
    { deploymentId: deploymentId! },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  );

  return (
    <>
      <Heading variant="h2" className="mb-4 text-xl">
        Logs
      </Heading>
      <ol className="font-mono w-full overflow-auto bg-muted p-4 rounded space-y-1">
        {deploymentLogs.events.map((event, index) => {
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

export const AssetsTabContents = () => {
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

  const handleDownload = (assetId: string, assetName: string) => {
    const downloadURL = new URL("/rpc/assets.serveOpenAPIv3", getServerURL());
    downloadURL.searchParams.set("id", assetId);
    downloadURL.searchParams.set("project_id", deployment.projectId);

    const link = document.createElement("a");
    link.href = downloadURL.toString();
    link.download = assetName;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  return (
    <>
      <Heading variant="h2" className="mb-4 text-xl">
        Assets
      </Heading>
      <ul className="flex gap-2 flex-wrap">
        {deployment.openapiv3Assets.map((asset) => {
          const downloadURL = new URL(
            "/rpc/assets.serveOpenAPIv3",
            getServerURL(),
          );
          downloadURL.searchParams.set("id", asset.assetId);
          downloadURL.searchParams.set("project_id", deployment.projectId);

          return (
            <li key={asset.id}>
              <MiniCard className="w-64">
                <MiniCard.Title className="truncate max-w-48">
                  <FileCodeIcon size={16} className="inline mr-2" />
                  {asset.name}
                </MiniCard.Title>
                <MiniCard.Description>OpenAPI Document</MiniCard.Description>
                <MiniCard.Actions
                  actions={[
                    {
                      label: "Download",
                      icon: "download",
                      onClick: () => handleDownload(asset.assetId, asset.name),
                    },
                  ]}
                />
              </MiniCard>
            </li>
          );
        })}
      </ul>
    </>
  );
};

export const ToolsTabContents = ({
  deploymentId,
}: {
  deploymentId: string;
}) => {
  return (
    <>
      <Heading variant="h2" className="mb-4 text-xl">
        Tools
      </Heading>
      <ToolsList deploymentId={deploymentId} />
    </>
  );
};
