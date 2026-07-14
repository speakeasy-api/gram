import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { DotTable } from "@/components/ui/dot-table";
import { ViewToggle } from "@/components/ui/view-toggle";
import { useViewMode } from "@/components/ui/use-view-mode";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useCatalogIconMap } from "./sources-hooks";
import { remoteMcpRouteParam, tunneledMcpRouteParam } from "@/lib/sources";
import { TUNNELED_MCP_FEATURE_FLAG } from "@/lib/tunneledMcp";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListAssets } from "@gram/client/react-query/listAssets.js";
import { useListTools } from "@gram/client/react-query/listTools.js";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useTunneledMcpServers } from "@gram/client/react-query/tunneledMcpServers.js";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Dialog } from "@/components/ui/dialog";
import { ChevronDown, CircleAlert, History, Plus } from "lucide-react";
import { useMemo } from "react";
import { toast } from "sonner";
import { create } from "zustand";
import { AddSourceMenuItems } from "./AddSourceMenuItems";
import { RemoveSourceDialogContent } from "./RemoveSourceDialogContent";
import { NamedAsset, SourceCard, SourceCardSkeleton } from "./SourceCard";
import { SourcesEmptyState } from "./SourcesEmptyState";
import { SourceTableRow } from "./SourceTableRow";
import { UploadOpenApiDialogContent } from "./UploadOpenApiDialogContent";
import { useFailedDeploymentSources } from "./useFailedDeploymentSources";
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

function sourcesDescription(
  isFunctionsEnabled: boolean,
  isTunneledMcpEnabled: boolean,
): string {
  if (isFunctionsEnabled && isTunneledMcpEnabled) {
    return "Remote MCPs, tunneled MCP servers, third-party MCP servers on the catalog, OpenAPI documents, and functions deployed in your project to power tools.";
  }
  if (isFunctionsEnabled) {
    return "Remote MCPs, third-party MCP servers on the catalog, OpenAPI documents, and functions deployed in your project to power tools.";
  }
  if (isTunneledMcpEnabled) {
    return "Remote MCPs, tunneled MCP servers, third-party MCP servers on the catalog, and OpenAPI documents deployed in your project to power tools.";
  }
  return "Remote MCPs, third-party MCP servers on the catalog, and OpenAPI documents deployed in your project to power tools.";
}

export default function Sources(): JSX.Element {
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;
  const isTunneledMcpEnabled =
    telemetry.isFeatureEnabled(TUNNELED_MCP_FEATURE_FLAG) ?? false;

  const {
    data: deploymentResult,
    refetch,
    isLoading: isLoadingDeployment,
  } = useLatestDeployment();
  const { data: assets, refetch: refetchAssets } = useListAssets();
  const { data: remoteMcpServersResult, isLoading: isLoadingRemoteMcp } =
    useRemoteMcpServers();
  const { data: tunneledMcpServersResult, isLoading: isLoadingTunneledMcp } =
    useTunneledMcpServers(undefined, undefined, {
      enabled: isTunneledMcpEnabled,
    });
  const catalogIconMap = useCatalogIconMap();
  const deployment = deploymentResult?.deployment;
  // Remote/tunneled sources bypass deployments, so page loading waits on their own queries.
  const isLoading =
    isLoadingDeployment ||
    isLoadingRemoteMcp ||
    (isTunneledMcpEnabled && isLoadingTunneledMcp);

  const [viewMode, setViewMode] = useViewMode();
  const toolCountsBySource = useToolCountsBySource();
  const assetsCausingFailure = useUnusedAssetIds();
  const {
    dialogState,
    openRemoveSource,
    openUploadOpenApi,
    openViewAsset,
    closeDialog,
  } = useDialogStore();

  const allSources: NamedAsset[] = useMemo(() => {
    // OpenAPI and Function sources need assets data
    const openApiSources =
      deployment && assets
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

    const functionSources =
      deployment && assets
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
    const externalMcpSources = (deployment?.externalMcps ?? []).map(
      (externalMcp) => ({
        id: externalMcp.id,
        deploymentAssetId: externalMcp.id,
        name: externalMcp.name,
        slug: externalMcp.slug,
        type: "externalmcp" as const,
        organizationMcpCollectionRegistryId:
          externalMcp.organizationMcpCollectionRegistryId,
        registryId: externalMcp.registryId,
        iconUrl: catalogIconMap.get(externalMcp.registryServerSpecifier),
      }),
    );

    // Remote MCP servers are project-scoped, not deployment-bound, so they're
    // fetched independently. The route slug prefers the server's slug column
    // for human-friendly URLs and falls back to the id.
    const remoteMcpSources: NamedAsset[] = (
      remoteMcpServersResult?.remoteMcpServers ?? []
    ).map((server) => ({
      id: server.id,
      deploymentAssetId: server.id,
      slug: remoteMcpRouteParam(server),
      name: server.name,
      url: server.url,
      type: "remotemcp" as const,
    }));

    const tunneledMcpSources: NamedAsset[] = isTunneledMcpEnabled
      ? (tunneledMcpServersResult?.tunneledMcpServers ?? []).map((server) => ({
          id: server.id,
          deploymentAssetId: server.id,
          slug: tunneledMcpRouteParam(server),
          name: server.name,
          type: "tunneledmcp" as const,
          createdAt: server.createdAt,
          updatedAt: server.updatedAt,
        }))
      : [];

    return [
      ...openApiSources,
      ...functionSources,
      ...externalMcpSources,
      ...remoteMcpSources,
      ...tunneledMcpSources,
    ];
  }, [
    deployment,
    assets,
    catalogIconMap,
    remoteMcpServersResult,
    tunneledMcpServersResult,
    isTunneledMcpEnabled,
  ]);

  const removeSource = async (
    assetId: string,
    type: "openapi" | "function" | "externalmcp",
  ) => {
    try {
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          nonBlocking: true,
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

  // Empty state covers all source types — a project can be deployment-empty
  // but still have remote MCP sources, in which case we want to show the
  // regular index, not the onboarding empty state.
  if (!isLoading && allSources.length === 0) {
    return (
      <>
        <SourcesEmptyState isTunneledMcpEnabled={isTunneledMcpEnabled} />
        {/* Render remove dialog in empty state to allow graceful close animation when deleting last source */}
        <Dialog
          open={dialogState.type === "remove-source"}
          onOpenChange={(open) => {
            void (!open && closeDialog());
          }}
        >
          <Dialog.Content className="max-w-2xl!">
            {dialogState.type === "remove-source" &&
              dialogState.asset.type !== "remotemcp" &&
              dialogState.asset.type !== "tunneledmcp" && (
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
    void refetch();
    void refetchAssets();
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title>Sources</Page.Section.Title>
        <Page.Section.Description className="max-w-2xl">
          {sourcesDescription(isFunctionsEnabled, isTunneledMcpEnabled)}
        </Page.Section.Description>
        <Page.Section.CTA>
          <ViewToggle value={viewMode} onChange={setViewMode} />
        </Page.Section.CTA>
        <Page.Section.CTA>
          <DeploymentsButton deploymentId={deployment?.id} />
        </Page.Section.CTA>
        <Page.Section.CTA>
          <RequireScope scope="project:write" level="component">
            {({ disabled }) => (
              <DropdownMenu>
                <DropdownMenuTrigger asChild disabled={disabled}>
                  <Button variant="secondary">
                    <Button.LeftIcon>
                      <Plus className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Add Source</Button.Text>
                    <Button.RightIcon>
                      <ChevronDown className="h-4 w-4" />
                    </Button.RightIcon>
                  </Button>
                </DropdownMenuTrigger>
                {!disabled && (
                  <DropdownMenuContent align="end" className="w-[320px] p-1">
                    <AddSourceMenuItems
                      isFunctionsEnabled={isFunctionsEnabled}
                      isTunneledMcpEnabled={isTunneledMcpEnabled}
                    />
                  </DropdownMenuContent>
                )}
              </DropdownMenu>
            )}
          </RequireScope>
        </Page.Section.CTA>
        <Page.Section.Body>
          {isLoading ? (
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
              <SourceCardSkeleton />
              <SourceCardSkeleton />
              <SourceCardSkeleton />
            </div>
          ) : viewMode === "grid" ? (
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
              {allSources?.map((asset: NamedAsset) => (
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
              ))}
            </div>
          ) : (
            <DotTable
              headers={[
                { label: "Name" },
                { label: "Type" },
                { label: "Tools" },
                { label: "Created" },
                { label: "Updated" },
                { label: "" },
                { label: "", className: "text-right" },
              ]}
            >
              {allSources?.map((asset: NamedAsset) => (
                <SourceTableRow
                  key={asset.id}
                  asset={asset}
                  causingFailure={assetsCausingFailure.has(
                    asset.deploymentAssetId,
                  )}
                  toolCount={
                    toolCountsBySource.get(asset.deploymentAssetId) ?? 0
                  }
                  deploymentId={deployment?.id}
                  handleRemove={() => openRemoveSource(asset)}
                  handleViewAsset={() => openViewAsset(asset)}
                  setChangeDocumentTargetSlug={openUploadOpenApi}
                />
              ))}
            </DotTable>
          )}
          <Dialog
            open={dialogState.type !== "closed"}
            onOpenChange={(open) => {
              void (!open && closeDialog());
            }}
          >
            <Dialog.Content
              className={
                dialogState.type === "view-asset"
                  ? "h-[90vh] min-w-[80vw]"
                  : "max-w-2xl!"
              }
            >
              {dialogState.type === "remove-source" &&
                dialogState.asset.type !== "remotemcp" &&
                dialogState.asset.type !== "tunneledmcp" && (
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
              {dialogState.type === "view-asset" &&
                dialogState.asset.type !== "remotemcp" &&
                dialogState.asset.type !== "tunneledmcp" && (
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

/**
 * Hook to count tools per source (keyed by deploymentAssetId).
 * OpenAPI tools are matched by openapiv3DocumentId, function tools by assetId,
 * and external MCP tools by slug.
 */
const useToolCountsBySource = () => {
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

  return useMemo(() => {
    const counts = new Map<string, number>();
    if (!toolsList.data) return counts;

    const deployment = latestDeployment.data?.deployment;
    if (!deployment) return counts;

    for (const tool of toolsList.data.tools) {
      // OpenAPI tools → match by openapiv3DocumentId to deployment asset id
      const docId = tool.httpToolDefinition?.openapiv3DocumentId;
      if (docId) {
        const match = deployment.openapiv3Assets.find((a) => a.id === docId);
        if (match) {
          counts.set(match.id, (counts.get(match.id) ?? 0) + 1);
        }
      }

      // Function tools → match by assetId to deployment functions asset id
      const funcAssetId = tool.functionToolDefinition?.assetId;
      if (funcAssetId) {
        for (const fa of deployment.functionsAssets ?? []) {
          if (fa.assetId === funcAssetId) {
            counts.set(fa.id, (counts.get(fa.id) ?? 0) + 1);
          }
        }
      }

      // External MCP tools → match by slug to external MCP id
      const extSlug = tool.externalMcpToolDefinition?.slug;
      if (extSlug) {
        const match = (deployment.externalMcps ?? []).find(
          (m) => m.slug === extSlug,
        );
        if (match) {
          counts.set(match.id, (counts.get(match.id) ?? 0) + 1);
        }
      }
    }

    return counts;
  }, [toolsList.data, latestDeployment.data]);
};

function DeploymentsButton({ deploymentId }: { deploymentId?: string }) {
  const routes = useRoutes();
  const failedDeployment = useFailedDeploymentSources();

  if (failedDeployment.hasFailures && deploymentId) {
    return (
      <a href={routes.deployments.deployment.href(deploymentId)}>
        <Button variant="secondary" className="text-destructive">
          <Button.LeftIcon>
            <CircleAlert className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Deployment Errors</Button.Text>
        </Button>
      </a>
    );
  }

  return (
    <a href={routes.deployments.href()}>
      <Button variant="secondary">
        <Button.LeftIcon>
          <History className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Deployments</Button.Text>
      </Button>
    </a>
  );
}
