import { Page } from "@/components/page-layout";
import { MiniCards } from "@/components/ui/card-mini";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { cn } from "@/lib/utils";
import { useDeploymentLogsSummary } from "@/pages/deployments/deployment/Deployment";
import { useRoutes } from "@/routes";
import {
  useLatestDeployment,
  useListAssets,
  useListTools,
} from "@gram/client/react-query/index.js";
// import { Dialog } from "@/components/ui/dialog";
import { Button, Dialog, Icon } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import { useMemo } from "react";
import { toast } from "@/lib/toast";
import { create } from "zustand";
import AddSourceDialogContent from "./AddSourceDialogContent";
import { AttachEnvironmentDialogContent } from "./AttachEnvironmentDialogContent";
import { RemoveSourceDialogContent } from "./RemoveSourceDialogContent";
import { NamedAsset, SourceCard } from "./SourceCard";
import { SourcesEmptyState } from "./SourcesEmptyState";
import { UploadOpenApiDialogContent } from "./UploadOpenApiDialogContent";
import { ViewAssetDialogContent } from "./ViewAssetDialogContent";

type DialogState =
  | { type: "closed" }
  | { type: "add-source" }
  | { type: "remove-source"; asset: NamedAsset }
  | { type: "upload-openapi"; documentSlug: string }
  | { type: "attach-environment"; asset: NamedAsset }
  | { type: "view-asset"; asset: NamedAsset };

interface DialogStore {
  dialogState: DialogState;
  openAddSource: () => void;
  openRemoveSource: (asset: NamedAsset) => void;
  openUploadOpenApi: (documentSlug: string) => void;
  openAttachEnvironment: (asset: NamedAsset) => void;
  openViewAsset: (asset: NamedAsset) => void;
  closeDialog: () => void;
}

const useDialogStore = create<DialogStore>((set) => ({
  dialogState: { type: "closed" },
  openAddSource: () => set({ dialogState: { type: "add-source" } }),
  openRemoveSource: (asset) =>
    set({ dialogState: { type: "remove-source", asset } }),
  openUploadOpenApi: (documentSlug) =>
    set({ dialogState: { type: "upload-openapi", documentSlug } }),
  openAttachEnvironment: (asset) =>
    set({ dialogState: { type: "attach-environment", asset } }),
  openViewAsset: (asset) => set({ dialogState: { type: "view-asset", asset } }),
  closeDialog: () => set({ dialogState: { type: "closed" } }),
}));

export function useDeploymentIsEmpty() {
  const { data: deploymentResult, isLoading } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  if (isLoading) {
    return false;
  }

  return (
    !deployment ||
    (deployment.openapiv3Assets.length === 0 &&
      (deployment.functionsAssets?.length ?? 0) === 0 &&
      deployment.packages.length === 0 &&
      (deployment.externalMcps?.length ?? 0) === 0)
  );
}

export default function Sources() {
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  const { data: deploymentResult, refetch, isLoading } = useLatestDeployment();
  const { data: assets, refetch: refetchAssets } = useListAssets();
  const deployment = deploymentResult?.deployment;

  const assetsCausingFailure = useUnusedAssetIds();
  const {
    dialogState,
    openAddSource,
    openRemoveSource,
    openUploadOpenApi,
    openAttachEnvironment,
    openViewAsset,
    closeDialog,
  } = useDialogStore();
  const deploymentIsEmpty = useDeploymentIsEmpty();

  const allSources: NamedAsset[] = useMemo(() => {
    if (!deployment || !assets) {
      return [];
    }

    const openApiSources = deployment.openapiv3Assets
      .map((deploymentAsset) => {
        const asset = assets.assets.find(
          (a) => a.id === deploymentAsset.assetId,
        );
        if (!asset) {
          console.error(`Asset ${deploymentAsset.assetId} not found`);
          return null;
        }
        return {
          ...asset,
          deploymentAssetId: deploymentAsset.id,
          name: deploymentAsset.name,
          slug: deploymentAsset.slug,
          type: "openapi" as const,
        };
      })
      .filter((source) => source !== null);

    const functionSources = (deployment.functionsAssets ?? [])
      .map((deploymentAsset) => {
        const asset = assets.assets.find(
          (a) => a.id === deploymentAsset.assetId,
        );
        if (!asset) {
          console.error(`Asset ${deploymentAsset.assetId} not found`);
          return null;
        }
        return {
          ...asset,
          deploymentAssetId: deploymentAsset.id,
          name: deploymentAsset.name,
          slug: deploymentAsset.slug,
          type: "function" as const,
        };
      })
      .filter((source) => source !== null);

    const externalMcpSources = (deployment.externalMcps ?? []).map(
      (externalMcp) => ({
        id: externalMcp.id,
        deploymentAssetId: externalMcp.id,
        name: externalMcp.name,
        slug: externalMcp.slug,
        type: "externalmcp" as const,
        registryId: externalMcp.registryId,
      }),
    );

    return [...openApiSources, ...functionSources, ...externalMcpSources];
  }, [deployment, assets]);

  const removeSource = async (
    assetId: string,
    type: "openapi" | "function" | "externalmcp",
  ) => {
    try {
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          ...(type === "openapi"
            ? { excludeOpenapiv3Assets: [assetId] }
            : type === "function"
              ? { excludeFunctions: [assetId] }
              : { excludeExternalMcps: [assetId] }),
        },
      });

      await Promise.all([refetch(), refetchAssets()]);

      const typeLabel =
        type === "openapi"
          ? "API"
          : type === "function"
            ? "Function"
            : "External MCP";
      toast.success(`${typeLabel} source deleted successfully`, { persist: true });
    } catch (error) {
      console.error(`Failed to delete ${type} source:`, error);
      const typeLabel =
        type === "openapi"
          ? "API"
          : type === "function"
            ? "function"
            : "external MCP";
      toast.error(`Failed to delete ${typeLabel} source. Please try again.`, { persist: true });
    }
  };

  if (!isLoading && deploymentIsEmpty) {
    return (
      <>
        <SourcesEmptyState onNewUpload={openAddSource} />
        <Dialog
          open={dialogState.type === "add-source"}
          onOpenChange={(open) => !open && closeDialog()}
        >
          <Dialog.Content className="max-w-2xl!">
            <AddSourceDialogContent onCompletion={closeDialog} />
          </Dialog.Content>
        </Dialog>
        {/* Render remove dialog in empty state to allow graceful close animation when deleting last source */}
        <Dialog
          open={dialogState.type === "remove-source"}
          onOpenChange={(open) => !open && closeDialog()}
        >
          <Dialog.Content className="max-w-2xl!">
            {dialogState.type === "remove-source" && (
              <RemoveSourceDialogContent
                asset={dialogState.asset}
                onConfirmRemoval={removeSource}
                onClose={closeDialog}
              />
            )}
          </Dialog.Content>
        </Dialog>
      </>
    );
  }

  const handleDialogSuccess = () => {
    closeDialog();
    refetch();
    refetchAssets();
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title>Sources</Page.Section.Title>
        <Page.Section.Description>
          {isFunctionsEnabled
            ? "OpenAPI documents and Gram Functions providing tools for your toolsets"
            : "OpenAPI documents providing tools for your toolsets"}
        </Page.Section.Description>
        <DeploymentHistoryCTA deploymentId={deployment?.id} />
        <Page.Section.CTA>
          <Button onClick={openAddSource} variant="secondary">
            <Button.LeftIcon>
              <Plus className="w-4 h-4" />
            </Button.LeftIcon>
            <Button.Text>Add Source</Button.Text>
          </Button>
        </Page.Section.CTA>
        <Page.Section.Body>
          <MiniCards isLoading={isLoading}>
            {allSources?.map((asset: NamedAsset) => (
              <SourceCard
                key={asset.id}
                asset={asset}
                causingFailure={assetsCausingFailure.has(
                  asset.deploymentAssetId,
                )}
                handleRemove={() => openRemoveSource(asset)}
                handleAttachEnvironment={() => openAttachEnvironment(asset)}
                handleViewAsset={() => openViewAsset(asset)}
                setChangeDocumentTargetSlug={openUploadOpenApi}
              />
            ))}
          </MiniCards>
          <Dialog
            open={dialogState.type !== "closed"}
            onOpenChange={(open) => !open && closeDialog()}
          >
            <Dialog.Content
              className={
                dialogState.type === "view-asset"
                  ? "min-w-[80vw] h-[90vh]"
                  : "max-w-2xl!"
              }
            >
              {dialogState.type === "add-source" && (
                <AddSourceDialogContent onCompletion={closeDialog} />
              )}
              {dialogState.type === "remove-source" && (
                <RemoveSourceDialogContent
                  asset={dialogState.asset}
                  onConfirmRemoval={removeSource}
                  onClose={closeDialog}
                />
              )}
              {dialogState.type === "upload-openapi" && (
                <UploadOpenApiDialogContent
                  documentSlug={dialogState.documentSlug}
                  onClose={closeDialog}
                  onSuccess={handleDialogSuccess}
                />
              )}
              {dialogState.type === "attach-environment" && (
                <AttachEnvironmentDialogContent
                  asset={dialogState.asset}
                  onClose={closeDialog}
                />
              )}
              {dialogState.type === "view-asset" && (
                <ViewAssetDialogContent asset={dialogState.asset} />
              )}
            </Dialog.Content>
          </Dialog>
        </Page.Section.Body>
      </Page.Section>
    </>
  );
}

/**
 * Hook to identify asset IDs not referenced by any tools in the latest
 * deployment.
 */
const useUnusedAssetIds = () => {
  const latestDeployment = useLatestDeployment();
  const toolsList = useListTools(
    {
      deploymentId: latestDeployment.data?.deployment?.id ?? "",
    },
    undefined,
    {
      enabled: !!latestDeployment.data?.deployment?.id,
    },
  );

  const unusedAssetIds: Set<string> = useMemo(() => {
    const deployment = latestDeployment.data?.deployment;

    if (!toolsList.data || !deployment?.openapiv3Assets) {
      return new Set<string>();
    }

    // Build set of valid asset IDs (those referenced by tools)
    const validAssetIds = new Set(
      toolsList.data.tools
        .map((tool) => tool.httpToolDefinition?.openapiv3DocumentId)
        .filter((id): id is string => id != null),
    );

    // Find assets not referenced by any tool
    return new Set(
      deployment.openapiv3Assets
        .map((asset) => asset.id)
        .filter((id) => !validAssetIds.has(id)),
    );
  }, [toolsList.data, latestDeployment.data]);

  return unusedAssetIds;
};

function DeploymentHistoryCTA({ deploymentId }: { deploymentId?: string }) {
  const routes = useRoutes();
  const { data: deploymentResult } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;
  const deploymentLogsSummary = useDeploymentLogsSummary(deploymentId);

  if (!deployment || !deploymentLogsSummary) {
    return null;
  }

  const hasErrors = deploymentLogsSummary.errors > 0;

  const icon = hasErrors ? (
    <Icon name="triangle-alert" className="text-yellow-500" />
  ) : (
    <Icon name="history" className="text-muted-foreground" />
  );

  const deploymentFailed = deployment.status === "failed";
  let tooltip = deploymentFailed
    ? "Latest deployment failed"
    : "Latest deployment succeeded";

  if (deploymentLogsSummary.skipped > 0) {
    tooltip += ` (${deploymentLogsSummary.skipped} operations skipped)`;
  }

  return (
    <Page.Section.CTA>
      <SimpleTooltip tooltip={tooltip}>
        <a href={routes.deployments.deployment.href(deployment.id)}>
          <Button
            variant="tertiary"
            className={cn(
              hasErrors &&
                "text-yellow-600 dark:text-yellow-500 hover:bg-yellow-500/20!",
            )}
          >
            <Button.LeftIcon>{icon}</Button.LeftIcon>
            HISTORY
          </Button>
        </a>
      </SimpleTooltip>
    </Page.Section.CTA>
  );
}
