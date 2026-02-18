import { Page } from "@/components/page-layout";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { cn } from "@/lib/utils";
import { useInfiniteListMCPCatalog } from "@/pages/catalog/hooks";
import { useDeploymentLogsSummary } from "@/pages/deployments/deployment/Deployment";
import { useRoutes } from "@/routes";
import {
  useLatestDeployment,
  useListAssets,
  useListTools,
} from "@gram/client/react-query/index.js";
import {
  Button,
  Dialog,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import { ChevronDown, Code, FileCode, Plus, Server } from "lucide-react";
import { useMemo } from "react";
import { toast } from "sonner";
import { create } from "zustand";
import { RemoveSourceDialogContent } from "./RemoveSourceDialogContent";
import { NamedAsset, SourceCard, SourceCardSkeleton } from "./SourceCard";
import { SourcesEmptyState } from "./SourcesEmptyState";
import { UploadOpenApiDialogContent } from "./UploadOpenApiDialogContent";
import { ViewAssetDialogContent } from "./ViewAssetDialogContent";

type DialogState =
  | { type: "closed" }
  | { type: "remove-source"; asset: NamedAsset }
  | { type: "upload-openapi"; documentSlug: string }
  | { type: "view-asset"; asset: NamedAsset };

interface DialogStore {
  dialogState: DialogState;
  openRemoveSource: (asset: NamedAsset) => void;
  openUploadOpenApi: (documentSlug: string) => void;
  openViewAsset: (asset: NamedAsset) => void;
  closeDialog: () => void;
}

const useDialogStore = create<DialogStore>((set) => ({
  dialogState: { type: "closed" },
  openRemoveSource: (asset) =>
    set({ dialogState: { type: "remove-source", asset } }),
  openUploadOpenApi: (documentSlug) =>
    set({ dialogState: { type: "upload-openapi", documentSlug } }),
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

export const useCatalogIconMap = () => {
  const { data: catalogData } = useInfiniteListMCPCatalog();
  return useMemo(() => {
    if (!catalogData?.pages) {
      return new Map<string, string>();
    }
    return new Map(
      catalogData.pages.flatMap((page) =>
        page.servers.map((s) => [s.registrySpecifier, s.iconUrl!]),
      ),
    );
  }, [catalogData]);
};

export default function Sources() {
  const client = useSdkClient();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  const { data: deploymentResult, refetch, isLoading } = useLatestDeployment();
  const { data: assets, refetch: refetchAssets } = useListAssets();
  const catalogIconMap = useCatalogIconMap();
  const deployment = deploymentResult?.deployment;

  const assetsCausingFailure = useUnusedAssetIds();
  const {
    dialogState,
    openRemoveSource,
    openUploadOpenApi,
    openViewAsset,
    closeDialog,
  } = useDialogStore();
  const deploymentIsEmpty = useDeploymentIsEmpty();

  const allSources: NamedAsset[] = useMemo(() => {
    if (!deployment) {
      return [];
    }

    // OpenAPI and Function sources need assets data
    const openApiSources = assets
      ? deployment.openapiv3Assets
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
          .filter((source) => source !== null)
      : [];

    const functionSources = assets
      ? (deployment.functionsAssets ?? [])
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
          .filter((source) => source !== null)
      : [];

    // External MCP sources don't need assets data - they come directly from deployment
    const externalMcpSources = (deployment.externalMcps ?? []).map(
      (externalMcp) => ({
        id: externalMcp.id,
        deploymentAssetId: externalMcp.id,
        name: externalMcp.name,
        slug: externalMcp.slug,
        type: "externalmcp" as const,
        registryId: externalMcp.registryId,
        iconUrl: catalogIconMap.get(externalMcp.registryServerSpecifier),
      }),
    );

    return [...openApiSources, ...functionSources, ...externalMcpSources];
  }, [deployment, assets, catalogIconMap]);

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
      toast.success(`${typeLabel} source deleted successfully`);
    } catch (error) {
      console.error(`Failed to delete ${type} source:`, error);
      const typeLabel =
        type === "openapi"
          ? "API"
          : type === "function"
            ? "function"
            : "external MCP";
      toast.error(`Failed to delete ${typeLabel} source. Please try again.`);
    }
  };

  if (!isLoading && deploymentIsEmpty) {
    return (
      <>
        <SourcesEmptyState />
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
            ? "OpenAPI documents, Gram Functions, and third-party MCP servers providing tools for your project"
            : "OpenAPI documents and third-party MCP servers providing tools for your project"}
        </Page.Section.Description>
        <DeploymentsButton deploymentId={deployment?.id} />
        <Page.Section.CTA>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="secondary">
                <Button.LeftIcon>
                  <Plus className="w-4 h-4" />
                </Button.LeftIcon>
                <Button.Text>Add Source</Button.Text>
                <Button.RightIcon>
                  <ChevronDown className="w-4 h-4" />
                </Button.RightIcon>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-[320px] p-1">
              <DropdownMenuItem
                onSelect={() => routes.sources.addOpenAPI.goTo()}
                className="cursor-pointer flex items-start gap-3 p-2 rounded-md"
              >
                <div className="w-10 h-10 rounded-lg bg-blue-500/10 dark:bg-blue-500/20 flex items-center justify-center shrink-0">
                  <FileCode className="w-5 h-5 text-blue-600 dark:text-blue-400" />
                </div>
                <div className="flex flex-col gap-0.5">
                  <span className="font-medium">From your API</span>
                  <span className="text-xs text-muted-foreground">
                    Upload an OpenAPI spec to generate tools
                  </span>
                </div>
              </DropdownMenuItem>
              {isFunctionsEnabled && (
                <DropdownMenuItem
                  onSelect={() => routes.sources.addFunction.goTo()}
                  className="cursor-pointer flex items-start gap-3 p-2 rounded-md"
                >
                  <div className="w-10 h-10 rounded-lg bg-emerald-500/10 dark:bg-emerald-500/20 flex items-center justify-center shrink-0">
                    <Code className="w-5 h-5 text-emerald-600 dark:text-emerald-400" />
                  </div>
                  <div className="flex flex-col gap-0.5">
                    <span className="font-medium">Write custom code</span>
                    <span className="text-xs text-muted-foreground">
                      Create tools with TypeScript functions
                    </span>
                  </div>
                </DropdownMenuItem>
              )}
              <DropdownMenuItem
                onSelect={() => routes.sources.addFromCatalog.goTo()}
                className="cursor-pointer flex items-start gap-3 p-2 rounded-md"
              >
                <div className="w-10 h-10 rounded-lg bg-violet-500/10 dark:bg-violet-500/20 flex items-center justify-center shrink-0">
                  <Server className="w-5 h-5 text-violet-600 dark:text-violet-400" />
                </div>
                <div className="flex flex-col gap-0.5">
                  <span className="font-medium">Third party server</span>
                  <span className="text-xs text-muted-foreground">
                    Add pre-built servers from the catalog
                  </span>
                </div>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </Page.Section.CTA>
        <Page.Section.Body>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {isLoading ? (
              <>
                <SourceCardSkeleton />
                <SourceCardSkeleton />
                <SourceCardSkeleton />
              </>
            ) : (
              allSources?.map((asset: NamedAsset) => (
                <SourceCard
                  key={asset.id}
                  asset={asset}
                  causingFailure={assetsCausingFailure.has(
                    asset.deploymentAssetId,
                  )}
                  deploymentId={deployment?.id}
                  handleRemove={() => openRemoveSource(asset)}
                  handleViewAsset={() => openViewAsset(asset)}
                  setChangeDocumentTargetSlug={openUploadOpenApi}
                />
              ))
            )}
          </div>
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

function DeploymentsButton({ deploymentId }: { deploymentId?: string }) {
  const routes = useRoutes();
  const { data: deploymentResult } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;
  const deploymentLogsSummary = useDeploymentLogsSummary(deploymentId);

  const hasErrors = deploymentLogsSummary && deploymentLogsSummary.errors > 0;
  const deploymentFailed = deployment?.status === "failed";

  const icon = hasErrors ? (
    <Icon name="triangle-alert" className="text-yellow-500" />
  ) : (
    <Icon name="history" className="text-muted-foreground" />
  );

  let tooltip = "View deployment history";
  if (deployment && deploymentLogsSummary) {
    tooltip = deploymentFailed
      ? "Latest deployment failed"
      : "Latest deployment succeeded";

    if (deploymentLogsSummary.skipped > 0) {
      tooltip += ` (${deploymentLogsSummary.skipped} operations skipped)`;
    }
  }

  return (
    <Page.Section.CTA>
      <SimpleTooltip tooltip={tooltip}>
        <a href={routes.deployments.href()}>
          <Button
            variant="tertiary"
            className={cn(
              hasErrors &&
                "text-yellow-600 dark:text-yellow-500 hover:bg-yellow-500/20!",
            )}
          >
            <Button.LeftIcon>{icon}</Button.LeftIcon>
            Deployments
          </Button>
        </a>
      </SimpleTooltip>
    </Page.Section.CTA>
  );
}
