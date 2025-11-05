import { Page } from "@/components/page-layout";
import { MiniCards } from "@/components/ui/card-mini";
import { Dialog } from "@/components/ui/dialog";
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
import { Button, Icon } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import { useMemo, useReducer } from "react";
import { toast } from "sonner";
import AddSourceDialogContent from "./AddSourceDialogContent";
import { RemoveSourceDialogContent } from "./RemoveSourceDialogContent";
import { NamedAsset, SourceCard } from "./SourceCard";
import { SourcesEmptyState } from "./SourcesEmptyState";
import { UploadOpenApiDialogContent } from "./UploadOpenApiDialogContent";

type DialogState =
  | { type: "closed" }
  | { type: "add-source" }
  | { type: "remove-source"; asset: NamedAsset }
  | { type: "upload-openapi"; documentSlug: string };

type DialogAction =
  | { type: "dialog/open-add-source" }
  | { type: "dialog/open-remove-source"; payload: { asset: NamedAsset } }
  | { type: "dialog/open-upload-openapi"; payload: { documentSlug: string } }
  | { type: "dialog/close" };

function dialogReducer(state: DialogState, action: DialogAction): DialogState {
  switch (action.type) {
    case "dialog/open-add-source":
      return { type: "add-source" };
    case "dialog/open-remove-source":
      return { type: "remove-source", asset: action.payload.asset };
    case "dialog/open-upload-openapi":
      return {
        type: "upload-openapi",
        documentSlug: action.payload.documentSlug,
      };
    case "dialog/close":
      return { type: "closed" };
    default:
      return state;
  }
}

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
      deployment.packages.length === 0)
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
  const [dialogState, dispatch] = useReducer(dialogReducer, {
    type: "closed",
  });
  const deploymentIsEmpty = useDeploymentIsEmpty();

  const allSources: NamedAsset[] = useMemo(() => {
    if (!deployment || !assets) {
      return [];
    }

    const openApiSources = deployment.openapiv3Assets.map((deploymentAsset) => {
      const asset = assets.assets.find((a) => a.id === deploymentAsset.assetId);
      if (!asset) {
        throw new Error(`Asset ${deploymentAsset.assetId} not found`);
      }
      return {
        ...asset,
        deploymentAssetId: deploymentAsset.id,
        name: deploymentAsset.name,
        slug: deploymentAsset.slug,
        type: "openapi" as const,
      };
    });

    const functionSources = (deployment.functionsAssets ?? []).map(
      (deploymentAsset) => {
        const asset = assets.assets.find(
          (a) => a.id === deploymentAsset.assetId,
        );
        if (!asset) {
          throw new Error(`Asset ${deploymentAsset.assetId} not found`);
        }
        return {
          ...asset,
          deploymentAssetId: deploymentAsset.id,
          name: deploymentAsset.name,
          slug: deploymentAsset.slug,
          type: "function" as const,
        };
      },
    );

    return [...openApiSources, ...functionSources];
  }, [deployment, assets]);

  if (!isLoading && deploymentIsEmpty) {
    return (
      <>
        <SourcesEmptyState
          onNewUpload={() => dispatch({ type: "dialog/open-add-source" })}
        />
        <Dialog
          open={dialogState.type !== "closed"}
          onOpenChange={(open) => !open && dispatch({ type: "dialog/close" })}
        >
          <Dialog.Content className="max-w-2xl!">
            {dialogState.type === "add-source" && <AddSourceDialogContent />}
          </Dialog.Content>
        </Dialog>
      </>
    );
  }

  const handleDialogSuccess = () => {
    dispatch({ type: "dialog/close" });
    refetch();
    refetchAssets();
  };

  const removeSource = async (
    assetId: string,
    type: "openapi" | "function",
  ) => {
    try {
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          ...(type === "openapi"
            ? { excludeOpenapiv3Assets: [assetId] }
            : { excludeFunctions: [assetId] }),
        },
      });

      await Promise.all([refetch(), refetchAssets()]);

      toast.success(
        `${type === "openapi" ? "API" : "Function"} source deleted successfully`,
      );
    } catch (error) {
      console.error(`Failed to delete ${type} source:`, error);
      toast.error(
        `Failed to delete ${type === "openapi" ? "API" : "function"} source. Please try again.`,
      );
    }
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
          <Button
            onClick={() => dispatch({ type: "dialog/open-add-source" })}
            variant="secondary"
          >
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
                handleRemove={() =>
                  dispatch({
                    type: "dialog/open-remove-source",
                    payload: { asset },
                  })
                }
                handleApplyEnvironment={(assetId) => {
                  // TODO: Implement apply environment functionality
                  console.log("Apply environment for asset:", assetId);
                }}
                setChangeDocumentTargetSlug={(documentSlug) =>
                  dispatch({
                    type: "dialog/open-upload-openapi",
                    payload: { documentSlug },
                  })
                }
              />
            ))}
          </MiniCards>
          <Dialog
            open={dialogState.type !== "closed"}
            onOpenChange={(open) => !open && dispatch({ type: "dialog/close" })}
          >
            <Dialog.Content className="max-w-2xl!">
              {dialogState.type === "add-source" && <AddSourceDialogContent />}
              {dialogState.type === "remove-source" && (
                <RemoveSourceDialogContent
                  asset={dialogState.asset}
                  onConfirmRemoval={removeSource}
                />
              )}
              {dialogState.type === "upload-openapi" && (
                <UploadOpenApiDialogContent
                  documentSlug={dialogState.documentSlug}
                  onClose={() => dispatch({ type: "dialog/close" })}
                  onSuccess={handleDialogSuccess}
                />
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
