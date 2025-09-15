import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import { MiniCard, MiniCards } from "@/components/ui/card-mini";
import { Dialog } from "@/components/ui/dialog";
import { SkeletonCode } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { UpdatedAt } from "@/components/updated-at";
import FileUpload from "@/components/upload";
import { useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { cn, getServerURL } from "@/lib/utils";
import { useDeploymentLogsSummary } from "@/pages/deployments/Deployment";
import { UploadedDocument } from "@/pages/onboarding/Wizard";
import { useRoutes } from "@/routes";
import { Asset } from "@gram/client/models/components";
import {
  useLatestDeployment,
  useListAssets,
} from "@gram/client/react-query/index.js";
import { Icon } from "@speakeasy-api/moonshine";
import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router";
import { toast } from "sonner";
import {
  OnboardingContent,
  useOnboardingSteps,
} from "../../onboarding/Onboarding";
import { ApisEmptyState } from "./ApisEmptyState";

export default function OpenAPIDocuments() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <APIsContent />
      </Page.Body>
    </Page>
  );
}

type NamedAsset = Asset & {
  name: string;
  slug: string;
};

export function useDeploymentIsEmpty() {
  const { data: deploymentResult, isLoading } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  if (isLoading) {
    return false;
  }

  return (
    !deployment ||
    (deployment.openapiv3Assets.length === 0 &&
      deployment.packages.length === 0)
  );
}

export function APIsContent() {
  const client = useSdkClient();
  const routes = useRoutes();

  const { data: deploymentResult, refetch, isLoading } = useLatestDeployment();
  const { data: assets, refetch: refetchAssets } = useListAssets();
  const deployment = deploymentResult?.deployment;

  const [isDeploying, setIsDeploying] = useState(false);
  const [newDocumentDialogOpen, setNewDocumentDialogOpen] = useState(false);
  const [changeDocumentTargetSlug, setChangeDocumentTargetSlug] = useState<
    string | null
  >(null);

  const finishUpload = () => {
    setNewDocumentDialogOpen(false);
    setChangeDocumentTargetSlug(null);
    undoSpecUpload(); // Reset the file state
    refetch();
    refetchAssets();
  };

  const { handleSpecUpload, createDeployment, file, undoSpecUpload } =
    useOnboardingSteps();

  const deploymentIsEmpty = useDeploymentIsEmpty();
  const deploymentLogsSummary = useDeploymentLogsSummary(deployment?.id);

  const newDocumentDialog = (
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
        <OnboardingContent onOnboardingComplete={finishUpload} />
        <Dialog.Footer>
          <Button variant="tertiary"
            onClick={() => setNewDocumentDialogOpen(false)}
          >
            Back
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );

  const logsCta = useMemo(() => {
    if (!deployment || !deploymentLogsSummary) {
      return null;
    }

    const hasErrors = deploymentLogsSummary.errors > 0;

    const icon = hasErrors ? (
      <Icon name="triangle-alert" className="text-yellow-500" />
    ) : (
      <Icon name="history" className="text-muted-foreground" />
    );

    return (
      <a href={routes.deployments.deployment.href(deployment.id)}>
        <Page.Section.CTA
          variant="tertiary"
          className={cn(
            hasErrors &&
              "text-yellow-600 dark:text-yellow-500 hover:bg-yellow-500/20!"
          )}
        >
          {icon}
          HISTORY
        </Page.Section.CTA>
      </a>
    );
  }, [deployment, deploymentLogsSummary]);

  const deploymentAssets: NamedAsset[] = useMemo(() => {
    if (!deployment || !assets) {
      return [];
    }

    return deployment.openapiv3Assets.map((deploymentAsset) => {
      const asset = assets.assets.find((a) => a.id === deploymentAsset.assetId);
      if (!asset) {
        throw new Error(`Asset ${deploymentAsset.assetId} not found`);
      }
      return {
        ...asset,
        name: deploymentAsset.name,
        slug: deploymentAsset.slug,
      };
    });
  }, [deployment, assets]);

  if (!isLoading && deploymentIsEmpty) {
    return (
      <>
        <ApisEmptyState onNewUpload={() => setNewDocumentDialogOpen(true)} />
        {newDocumentDialog}
      </>
    );
  }

  const removeDocument = async (assetId: string) => {
    try {
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          excludeOpenapiv3Assets: [assetId],
        },
      });

      await Promise.all([refetch(), refetchAssets()]);

      toast.success("API source deleted successfully");
    } catch (error) {
      console.error("Failed to delete API source:", error);
      toast.error("Failed to delete API source. Please try again.");
    }
  };

  const deploySpecUpdate = async (documentSlug: string) => {
    setIsDeploying(true);
    await createDeployment(documentSlug); // Make sure we overwrite the current document by slug
    finishUpload();
    toast.success("OpenAPI document deployed");
    setIsDeploying(false);
  };

  return (
    <Page.Section>
      <Page.Section.Title>API Sources</Page.Section.Title>
      <Page.Section.Description>
        OpenAPI documents providing tools for your toolsets
      </Page.Section.Description>
      {logsCta}
      <Page.Section.CTA
        onClick={() => setNewDocumentDialogOpen(true)}
        variant="secondary"
      >
        <Button.LeftIcon>
          <Plus className="w-4 h-4" />
        </Button.LeftIcon>
        <Button.Text>Add API</Button.Text>
      </Page.Section.CTA>
      <Page.Section.Body>
        <MiniCards isLoading={isLoading}>
          {deploymentAssets?.map((asset: NamedAsset) => (
            <OpenAPICard
              key={asset.id}
              asset={asset}
              removeDocument={removeDocument}
              setChangeDocumentTargetSlug={setChangeDocumentTargetSlug}
            />
          ))}
        </MiniCards>
        {newDocumentDialog}
        <Dialog
          open={changeDocumentTargetSlug !== null}
          onOpenChange={(open) => {
            if (!open) {
              setChangeDocumentTargetSlug(null);
              undoSpecUpload(); // Reset the file state when dialog closes
            }
          }}
        >
          <Dialog.Content className="max-w-2xl!">
            <Dialog.Header>
              <Dialog.Title>New OpenAPI Version</Dialog.Title>
              <Dialog.Description>
                You are creating a new version of document{" "}
                {changeDocumentTargetSlug}
              </Dialog.Description>
            </Dialog.Header>
            {!file ? (
              <FileUpload
                onUpload={handleSpecUpload}
                allowedExtensions={["yaml", "yml", "json"]}
              />
            ) : (
              <UploadedDocument
                file={file}
                onReset={undoSpecUpload}
                defaultExpanded
              />
            )}
            <Dialog.Footer>
              <Button variant="tertiary"
                onClick={() => {
                  setChangeDocumentTargetSlug(null);
                  undoSpecUpload(); // Reset the file state when dialog closes
                }}
              >
                Back
              </Button>
              <Button
                onClick={() => deploySpecUpdate(changeDocumentTargetSlug!)}
                disabled={!file || isDeploying || !changeDocumentTargetSlug}
              >
                {isDeploying && <Spinner />}
                {isDeploying ? "Deploying..." : "Deploy"}
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      </Page.Section.Body>
    </Page.Section>
  );
}

function OpenAPICard({
  asset,
  removeDocument,
  setChangeDocumentTargetSlug,
}: {
  asset: NamedAsset;
  removeDocument: (assetId: string) => void;
  setChangeDocumentTargetSlug: (slug: string) => void;
}) {
  const [documentViewOpen, setDocumentViewOpen] = useState(false);

  return (
    <MiniCard key={asset.id}>
      <MiniCard.Title
        onClick={() => setDocumentViewOpen(true)}
        className="cursor-pointer"
      >
        {asset.name}
      </MiniCard.Title>
      <MiniCard.Description>
        <UpdatedAt date={asset.updatedAt} italic={false} className="text-xs" />
      </MiniCard.Description>
      <MiniCard.Actions
        actions={[
          {
            label: "View",
            onClick: () => setDocumentViewOpen(true),
            icon: "eye",
          },
          {
            label: "Update",
            onClick: () => setChangeDocumentTargetSlug(asset.slug),
            icon: "upload",
          },
          {
            label: "Delete",
            onClick: () => removeDocument(asset.id),
            icon: "trash",
            destructive: true,
          },
        ]}
      />
      <AssetViewDialog
        asset={asset}
        open={documentViewOpen}
        onOpenChange={setDocumentViewOpen}
      />
    </MiniCard>
  );
}

function AssetViewDialog({
  asset,
  open,
  onOpenChange,
}: {
  asset: NamedAsset;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  // const client = useSdkClient();
  const { projectSlug } = useParams();
  const project = useProject();
  const [content, setContent] = useState<string>("");
  const [isLoading, setIsLoading] = useState(false);

  const downloadURL = new URL("/rpc/assets.serveOpenAPIv3", getServerURL());
  downloadURL.searchParams.set("id", asset.id);
  downloadURL.searchParams.set("project_id", project.id);

  useEffect(() => {
    if (!open || !projectSlug) {
      setContent("");
      return;
    }

    fetch(downloadURL, {
      credentials: "same-origin",
    }).then((assetData) => {
      if (!assetData.ok) {
        setContent("");
        return;
      }
      setIsLoading(true);
      assetData.text().then((content) => {
        setContent(content);
        setIsLoading(false);
      });
    });
  }, [open, projectSlug]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="min-w-[80vw] h-[90vh]">
        <Dialog.Header>
          <Dialog.Title>{asset.name}</Dialog.Title>
          <Dialog.Description>
            <UpdatedAt date={asset.updatedAt} italic={false} />
          </Dialog.Description>
        </Dialog.Header>
        {isLoading ? (
          <SkeletonCode />
        ) : (
          <CodeBlock className="overflow-auto">{content}</CodeBlock>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
