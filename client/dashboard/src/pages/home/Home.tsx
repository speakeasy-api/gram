import { Page } from "@/components/page-layout";
import { Card, Cards } from "@/components/ui/card";
import { formatDistanceToNow } from "date-fns";
import { Suspense, useState } from "react";
import { Stack } from "@speakeasy-api/moonshine";
import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import {
  useDeploymentSuspense,
  useListToolsSuspense,
  useLatestDeployment,
} from "@gram/client/react-query/index.js";
import { HTTPToolDefinition } from "@gram/client/models/components/httptooldefinition";
import { GetDeploymentResult } from "@gram/client/models/components/getdeploymentresult";
import { useProject } from "@/contexts/Auth";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  TooltipProvider,
} from "@/components/ui/tooltip";
import { Heading } from "@/components/ui/heading";
import { CreateThingCard } from "../toolsets/Toolsets";
import { Dialog } from "@/components/ui/dialog";
import { OnboardingContent } from "../onboarding/Onboarding";
import { Button } from "@/components/ui/button";
import { NameAndSlug } from "@/components/name-and-slug";

function DeploymentCards() {
  const project = useProject();
  const { data: deployment, refetch } = useLatestDeployment({
    gramProject: project.slug,
  });

  if (!deployment?.deployment) {
    return (
      <Card>
        <Card.Content className="pt-6">No deployments found.</Card.Content>
      </Card>
    );
  }

  return (
    <Suspense fallback={<Cards loading={true} />}>
      <DeploymentTools
        deploymentId={deployment.deployment.id}
        onNewDeployment={refetch}
      />
    </Suspense>
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

function DeploymentTools({
  deploymentId,
  onNewDeployment,
}: {
  deploymentId: string;
  onNewDeployment: () => void;
}) {
  const project = useProject();

  const { data: deployment } = useDeploymentSuspense({
    gramProject: project.slug,
    id: deploymentId,
  });
  const { data: toolsData } = useListToolsSuspense({
    gramProject: project.slug,
    deploymentId: deploymentId,
  });

  const [newDocumentDialogOpen, setNewDocumentDialogOpen] = useState(false);
  const [changeDocumentTargetSlug, setChangeDocumentTargetSlug] =
    useState<string>();

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

  const finishUpload = () => {
    setNewDocumentDialogOpen(false);
    setChangeDocumentTargetSlug(undefined);
    onNewDeployment();
  };

  // TODO: We need to support this in the API
  const removeDocument = (slug: string) => {
    alert(`TODO: We need to support this ${slug}`);
  };

  return (
    <>
      <Heading variant="h3">OpenAPI Documents</Heading>
      <Cards>
        {deployment.openapiv3Assets.map(
          (asset: GetDeploymentResult["openapiv3Assets"][0]) => {
            const tools = toolsByDocument[asset.id] || [];
            const latestToolTimestamp =
              tools.length > 0
                ? new Date(
                    Math.max(
                      ...tools.map((t) => new Date(t.createdAt).getTime())
                    )
                  )
                : null;

            return (
              <Card key={asset.id}>
                <Card.Header>
                  <Stack direction="horizontal" gap={2} justify="space-between">
                    <Card.Title>
                      <NameAndSlug name={asset.name} slug={asset.slug} />
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
                  <Type muted variant="body" className="italic line-clamp-2">
                    {tools
                      .map((tool) => tool.name.replace(asset.slug + "_", ""))
                      .join(", ")}
                  </Type>
                </Card.Content>
                <Card.Footer className="justify-end">
                  <Stack direction="horizontal" gap={2}>
                    <Button
                      variant="destructiveGhost"
                      onClick={() => removeDocument(asset.slug)}
                      tooltip="Remove this document and related tools"
                      icon="trash"
                    >
                      Delete
                    </Button>
                    <Button
                      variant="secondary"
                      onClick={() => setChangeDocumentTargetSlug(asset.slug)}
                      tooltip="Upload a new version of this document"
                      icon="upload"
                    >
                      Update
                    </Button>
                  </Stack>
                </Card.Footer>
              </Card>
            );
          }
        )}
        <CreateThingCard onClick={() => setNewDocumentDialogOpen(true)}>
          + New OpenAPI Source
        </CreateThingCard>
      </Cards>
      <Dialog
        open={newDocumentDialogOpen}
        onOpenChange={setNewDocumentDialogOpen}
      >
        <Dialog.Content className="max-w-2xl!">
          <Dialog.Header>
            <Dialog.Title>New OpenAPI Source</Dialog.Title>
            <Dialog.Description>
              Upload a new OpenAPI document to use in addition to your existing
              documents.
            </Dialog.Description>
          </Dialog.Header>
          <OnboardingContent
            className="scale-95"
            onOnboardingComplete={finishUpload}
          />
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={() => setNewDocumentDialogOpen(false)}
            >
              Back
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
      <Dialog
        open={changeDocumentTargetSlug !== undefined}
        onOpenChange={() => setChangeDocumentTargetSlug(undefined)}
      >
        <Dialog.Content className="max-w-2xl!">
          <Dialog.Header>
            <Dialog.Title>New OpenAPI Version</Dialog.Title>
            <Dialog.Description>
              You are creating a new version of document{" "}
              {changeDocumentTargetSlug}
            </Dialog.Description>
          </Dialog.Header>
          <OnboardingContent
            existingDocumentSlug={changeDocumentTargetSlug}
            className="scale-95"
            onOnboardingComplete={finishUpload}
          />
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={() => setChangeDocumentTargetSlug(undefined)}
            >
              Back
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
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
        <Suspense fallback={<Cards loading={true} />}>
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
