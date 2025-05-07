import { NameAndSlug } from "@/components/name-and-slug";
import { Page } from "@/components/page-layout";
import { ToolsBadge } from "@/components/tools-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, Cards } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import { GetDeploymentResult } from "@gram/client/models/components/getdeploymentresult";
import { ToolEntry } from "@gram/client/models/components/toolentry.js";
import {
  useDeploymentSuspense,
  useLatestDeployment,
  useListIntegrations,
  useListToolsSuspense,
} from "@gram/client/react-query/index.js";
import { Stack } from "@speakeasy-api/moonshine";
import { formatDistanceToNow } from "date-fns";
import { CheckIcon } from "lucide-react";
import { Suspense, useState } from "react";
import { OnboardingContent } from "../onboarding/Onboarding";
import { CreateThingCard } from "../toolsets/Toolsets";

function DeploymentCards() {
  const { data: deployment, refetch } = useLatestDeployment();

  const deploymentEmpty =
    !deployment?.deployment ||
    (deployment.deployment.openapiv3Assets.length === 0 &&
      deployment.deployment.packages.length === 0);

  if (deploymentEmpty) {
    return <OnboardingContent onOnboardingComplete={() => refetch()} />;
  }

  return (
    <Suspense fallback={<Cards loading={true} />}>
      <DeploymentTools
        deploymentId={deployment.deployment!.id}
        onNewDeployment={refetch}
      />
    </Suspense>
  );
}

function DeploymentTools({
  deploymentId,
  onNewDeployment,
}: {
  deploymentId: string;
  onNewDeployment: () => void;
}) {
  const routes = useRoutes();
  const client = useSdkClient();
  const { data: deployment } = useDeploymentSuspense({
    id: deploymentId,
  });
  const { data: toolsData } = useListToolsSuspense({
    deploymentId: deploymentId,
  });

  const [newDocumentDialogOpen, setNewDocumentDialogOpen] = useState(false);
  const [changeDocumentTargetSlug, setChangeDocumentTargetSlug] =
    useState<string>();

  if (
    !deployment ||
    (deployment.openapiv3Assets.length === 0 &&
      deployment.packages.length === 0)
  ) {
    return (
      <Card>
        <Card.Content className="pt-6">
          Latest deployment is empty.
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

  const removeDocument = async (assetId: string) => {
    await client.deployments.evolveDeployment({
      evolveForm: {
        deploymentId: deploymentId,
        excludeOpenapiv3Assets: [assetId],
      },
    });

    onNewDeployment();
  };

  return (
    <>
      <Heading variant="h3">OpenAPI Documents</Heading>
      <Cards>
        {deployment.openapiv3Assets.map(
          (asset: GetDeploymentResult["openapiv3Assets"][0]) => {
            const tools = toolsByDocument[asset.id] || [];
            return (
              <DeploymentCard
                key={asset.id}
                tools={tools}
                asset={asset}
                removeDocument={removeDocument}
                setChangeDocumentTargetSlug={setChangeDocumentTargetSlug}
              />
            );
          }
        )}
        <CreateThingCard onClick={() => setNewDocumentDialogOpen(true)}>
          + New OpenAPI Source
        </CreateThingCard>
      </Cards>
      <Heading variant="h3">Third Party Integrations</Heading>
      <Cards>
        {deployment.packages.map((pkg) => (
          <PackageCard key={pkg.id} packageId={pkg.id} />
        ))}
        <CreateThingCard onClick={() => routes.integrations.goTo()}>
          + Add Integration
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

function DeploymentCard({
  tools,
  asset,
  removeDocument,
  setChangeDocumentTargetSlug,
}: {
  tools: ToolEntry[];
  asset: GetDeploymentResult["openapiv3Assets"][0];
  removeDocument: (assetId: string) => void;
  setChangeDocumentTargetSlug: (slug: string) => void;
}) {
  const latestToolTimestamp =
    tools.length > 0
      ? new Date(Math.max(...tools.map((t) => new Date(t.createdAt).getTime())))
      : null;

  return (
    <Card key={asset.id}>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify="space-between">
          <Card.Title>
            <NameAndSlug name={asset.name} slug={asset.slug} />
          </Card.Title>
          <ToolsBadge tools={tools} />
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
      <Card.Footer>
        <Stack direction="horizontal" gap={2}>
          <Button
            variant="destructiveGhost"
            onClick={() => removeDocument(asset.assetId)}
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

function PackageCard({ packageId }: { packageId: string }) {
  const routes = useRoutes();
  const { data: integrations } = useListIntegrations();

  const pkg = integrations?.integrations?.find(
    (i) => i.packageId === packageId
  );

  if (!pkg) {
    return null;
  }

  const handleDisable = async () => {
    routes.integrations.goTo();
  };

  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Card.Title>{pkg.packageTitle}</Card.Title>
          <div className="flex gap-2 items-center">
            <Badge className="h-6 flex items-center">Third Party</Badge>
            <ToolsBadge tools={pkg.toolNames} />
          </div>
        </Stack>
        <Stack direction="horizontal" gap={3} justify={"space-between"}>
          <Card.Description className="max-w-2/3">
            {pkg.packageSummary}
          </Card.Description>
          <Type variant="body" muted className="text-sm italic">
            {"Updated "}
            <HumanizeDateTime date={new Date(pkg.versionCreatedAt)} />
          </Type>
        </Stack>
      </Card.Header>
      <Card.Footer>
        <Button variant="outline" onClick={handleDisable}>
          <CheckIcon className="w-4 h-4" />
          Enabled
        </Button>
      </Card.Footer>
    </Card>
  );
}

function groupToolsByDocument(tools: ToolEntry[]) {
  return tools.reduce<Record<string, ToolEntry[]>>((groups, tool) => {
    const docId = tool.openapiv3DocumentId;
    if (!docId) return groups;

    if (!groups[docId]) {
      groups[docId] = [];
    }
    groups[docId].push(tool);
    return groups;
  }, {});
}
