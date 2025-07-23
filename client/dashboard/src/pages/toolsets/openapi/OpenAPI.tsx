import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { MiniCard, MiniCards } from "@/components/ui/card-mini";
import { Dialog } from "@/components/ui/dialog";
import { SkeletonCode } from "@/components/ui/skeleton";
import { UpdatedAt } from "@/components/updated-at";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { Asset } from "@gram/client/models/components";
import {
  useLatestDeployment,
  useListAssets,
} from "@gram/client/react-query/index.js";
import { useEffect, useState } from "react";
import { useParams } from "react-router";
import { OnboardingContent } from "../../onboarding/Onboarding";
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

export function APIsContent() {
  const client = useSdkClient();
  const { data: deploymentResult, refetch, isLoading } = useLatestDeployment();
  const { data: assets, refetch: refetchAssets } = useListAssets();
  const deployment = deploymentResult?.deployment;

  const [newDocumentDialogOpen, setNewDocumentDialogOpen] = useState(false);
  const [changeDocumentTargetSlug, setChangeDocumentTargetSlug] =
    useState<string | null>(null);

  const finishUpload = () => {
    setNewDocumentDialogOpen(false);
    setChangeDocumentTargetSlug(null);
    refetch();
    refetchAssets();
  };

  const deploymentIsEmpty =
    !deployment ||
    (deployment.openapiv3Assets.length === 0 &&
      deployment.packages.length === 0);

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
          <Button
            variant="secondary"
            onClick={() => setNewDocumentDialogOpen(false)}
          >
            Back
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );

  if (!isLoading && deploymentIsEmpty) {
    return (
      <>
        <ApisEmptyState onNewUpload={() => setNewDocumentDialogOpen(true)} />
        {newDocumentDialog}
      </>
    );
  }

  const removeDocument = async (assetId: string) => {
    await client.deployments.evolveDeployment({
      evolveForm: {
        deploymentId: deployment?.id,
        excludeOpenapiv3Assets: [assetId],
      },
    });

    refetch();
  };

  const usedAssets: NamedAsset[] =
    assets?.assets.flatMap((asset) => {
      const deploymentAsset = deployment?.openapiv3Assets.find(
        (a) => a.assetId === asset.id
      );
      return deploymentAsset
        ? [{ ...asset, name: deploymentAsset.name, slug: deploymentAsset.slug }]
        : [];
    }) || [];

  return (
    <Page.Section>
      <Page.Section.Title>API Sources</Page.Section.Title>
      <Page.Section.Description>
        OpenAPI documents providing tools for your toolsets
      </Page.Section.Description>
      <Page.Section.CTA
        onClick={() => setNewDocumentDialogOpen(true)}
        icon="plus"
        variant="secondary"
      >
        Add API
      </Page.Section.CTA>
      <Page.Section.Body>
        <MiniCards isLoading={isLoading}>
          {usedAssets?.map((asset: NamedAsset) => (
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
          onOpenChange={() => setChangeDocumentTargetSlug(null)}
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
              existingDocumentSlug={changeDocumentTargetSlug ?? undefined}
              onOnboardingComplete={finishUpload}
            />
            <Dialog.Footer>
              <Button
                variant="secondary"
                onClick={() => setChangeDocumentTargetSlug(null)}
              >
                Back
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
  const [content, setContent] = useState<string>("");
  const [isLoading, setIsLoading] = useState(false);

  const downloadURL = new URL("/rpc/assets.serveOpenAPIv3", getServerURL());
  downloadURL.searchParams.set("id", asset.id);

  useEffect(() => {
    if (!open || !projectSlug) {
      setContent("");
      return;
    }

    fetch(downloadURL, {
      headers: {
        "gram-project": projectSlug,
      },
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
