import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { formatDistanceToNow } from "date-fns";
import { Suspense } from "react";
import { Stack } from "@speakeasy-api/moonshine";
import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import {
  useListDeploymentsSuspense,
  useDeploymentSuspense,
  useListToolsSuspense,
} from "@gram/client/react-query/index.js";
import { HTTPToolDefinition } from "@gram/client/models/components/httptooldefinition";
import { GetDeploymentResult } from "@gram/client/models/components/getdeploymentresult";
import { useProject } from "@/contexts/Auth";
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from "@/components/ui/tooltip";

function DeploymentCards() {
  const project = useProject();
  const { data: deployments } = useListDeploymentsSuspense({
    gramProject: project.projectSlug,
  });
  const latestDeploymentId = deployments.items?.[0]?.id;

  if (!latestDeploymentId) {
    return (
      <Card>
        <Card.Content className="pt-6">No deployments found.</Card.Content>
      </Card>
    );
  }

  return (
    <Suspense fallback={<LoadingCards />}>
      <DeploymentTools deploymentId={latestDeploymentId} />
    </Suspense>
  );
}

function LoadingCards() {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      <Card>
        <Card.Header>
          <Stack direction="horizontal" gap={2} justify="space-between">
            <Skeleton className="h-6 w-40" />
            <Skeleton className="h-5 w-20" />
          </Stack>
          <Skeleton className="h-4 w-24 mt-2" />
        </Card.Header>
      </Card>
    </div>
  );
}

function ToolsTooltipContent({ tools }: { tools: HTTPToolDefinition[] }) {
  if (tools.length === 0) return null;
  
  return (
    <Stack gap={1}>
      {tools.map((tool) => (
        <p key={tool.id}>{tool.name}</p>
      ))}
    </Stack>
  );
}

function DeploymentTools({ deploymentId }: { deploymentId: string }) {
  const project = useProject();
  const { data: deployment } = useDeploymentSuspense({
    gramProject: project.projectSlug,
    id: deploymentId,
  });
  const { data: toolsData } = useListToolsSuspense({
    gramProject: project.projectSlug,
  });

  if (!deployment?.openapiv3Assets?.length) {
    return (
      <Card>
        <Card.Content className="pt-6">
          No OpenAPI documents found in the latest deployment.
        </Card.Content>
      </Card>
    );
  }

  const toolsByDocument = groupToolsByDocument(toolsData?.tools || []);

  return (
    <>
      <h1 className="mb-2">Documents</h1>
      <div className="grid grid-cols-1 gap-4">
        {deployment.openapiv3Assets.map(
          (asset: GetDeploymentResult["openapiv3Assets"][0]) => {
            const tools = toolsByDocument[asset.id] || [];
            const latestToolTimestamp = tools.length > 0 
              ? new Date(Math.max(...tools.map(t => new Date(t.createdAt).getTime())))
              : null;
            
            return (
              <Card key={asset.id}>
                <Card.Header>
                  <Stack direction="horizontal" gap={2} justify="space-between">
                    <Card.Title>
                      <Stack direction="horizontal" gap={2}>
                        <span>{asset.name}</span>
                        <span className="text-muted-foreground lowercase">
                          ({asset.slug})
                        </span>
                      </Stack>
                    </Card.Title>
                    {tools.length > 0 ? (
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Badge>{tools.length} Tools</Badge>
                          </TooltipTrigger>
                          <TooltipContent>
                            <div className="max-h-[300px] overflow-y-auto">
                              <ToolsTooltipContent tools={tools} />
                            </div>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    ) : (
                      <Badge>No Tools</Badge>
                    )}
                  </Stack>
                  {latestToolTimestamp && (
                    <Type variant="body" muted className="text-sm italic">
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span>
                              {"Updated "}
                              {formatDistanceToNow(latestToolTimestamp, {
                                addSuffix: true,
                              })}
                            </span>
                          </TooltipTrigger>
                          <TooltipContent>
                            {latestToolTimestamp.toString()}
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </Type>
                  )}
                </Card.Header>
                <Card.Content>
                </Card.Content>
              </Card>
            );
          }
        )}
      </div>
    </>
  );
}

export default function Home() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Suspense fallback={<LoadingCards />}>
          <DeploymentCards />
        </Suspense>
      </Page.Body>
    </Page>
  );
}


function groupToolsByDocument(tools: HTTPToolDefinition[]) {
  return tools.reduce<Record<string, HTTPToolDefinition[]>>((groups, tool) => {
    const docId = tool.openapiv3DocumentId;
    if (!docId) return groups;

    if (!groups[docId]) {
      groups[docId] = [];
    }
    groups[docId].push(tool);
    return groups;
  }, {});
}

