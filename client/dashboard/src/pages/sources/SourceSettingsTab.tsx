import { RemoveSourceDialogContent } from "@/components/sources/RemoveSourceDialogContent";
import { ViewSourceDialogContent } from "@/components/sources/ViewSourceDialogContent";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type {
  DeploymentFunctions,
  OpenAPIv3DeploymentAsset,
} from "@gram/client/models/components";
import {
  useLatestDeployment,
  useListAssets,
} from "@gram/client/react-query/index.js";
import { Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { Download, Eye, Trash2 } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

type Source = OpenAPIv3DeploymentAsset | DeploymentFunctions;

export function SourceSettingsTab({
  isOpenAPI,
  source,
}: {
  isOpenAPI: boolean;
  source: Source | null;
}) {
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  const client = useSdkClient();
  const project = useProject();
  const navigate = useNavigate();
  const routes = useRoutes();
  const { data: deployment, refetch } = useLatestDeployment();
  const { data: assetsData, refetch: refetchAssets } = useListAssets();

  const underlyingAsset =
    assetsData?.assets.find((a) => a.id === source?.assetId) ?? null;

  const handleDownload = () => {
    if (!source) return;
    const path = isOpenAPI
      ? "/rpc/assets.serveOpenAPIv3"
      : "/rpc/assets.serveFunction";
    const downloadURL = new URL(path, getServerURL());
    downloadURL.searchParams.set("id", source.assetId);
    downloadURL.searchParams.set("project_id", project.id);

    const link = document.createElement("a");
    link.href = downloadURL.toString();
    link.download = source.slug;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  const handleRemoveSource = async (
    assetId: string,
    type: "openapi" | "function" | "externalmcp",
  ) => {
    try {
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.deployment?.id,
          ...(type === "openapi"
            ? { excludeOpenapiv3Assets: [assetId] }
            : { excludeFunctions: [assetId] }),
        },
      });
      await Promise.all([refetch(), refetchAssets()]);
      const typeLabel = type === "openapi" ? "API" : "Function";
      toast.success(`${typeLabel} source deleted successfully`);
      navigate(routes.sources.href());
    } catch (error) {
      console.error(`Failed to delete ${type} source:`, error);
      const typeLabel = type === "openapi" ? "API" : "function";
      toast.error(`Failed to delete ${typeLabel} source. Please try again.`);
    }
  };

  const assetForDialog =
    source && underlyingAsset
      ? {
          ...underlyingAsset,
          deploymentAssetId: source.id,
          name: source.name,
          slug: source.slug,
          type: isOpenAPI ? ("openapi" as const) : ("function" as const),
        }
      : null;

  return (
    <>
      <div className="max-w-[1270px] mx-auto px-8 py-8 w-full space-y-8">
        <div>
          <Type variant="subheading" className="mb-4">
            Source Actions
          </Type>
          <Stack direction="horizontal" gap={3}>
            {!isOpenAPI && (
              <Button
                variant="secondary"
                size="md"
                onClick={() => setIsModalOpen(true)}
              >
                <Button.LeftIcon>
                  <Eye className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>View Manifest</Button.Text>
              </Button>
            )}
            <Button variant="secondary" size="md" onClick={handleDownload}>
              <Button.LeftIcon>
                <Download className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Download</Button.Text>
            </Button>
          </Stack>
        </div>

        <div className="border border-destructive/30 rounded-lg p-6">
          <Type variant="subheading" className="text-destructive mb-1">
            Danger Zone
          </Type>
          <Type muted small className="mb-4">
            Removing this source will remove it from the current deployment.
            This action cannot be undone.
          </Type>
          <Button
            variant="destructive-primary"
            size="md"
            onClick={() => setDeleteDialogOpen(true)}
          >
            <Button.LeftIcon>
              <Trash2 className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Delete Source</Button.Text>
          </Button>
        </div>
      </div>

      {!isOpenAPI && (
        <Dialog open={isModalOpen} onOpenChange={setIsModalOpen}>
          <Dialog.Content className="min-w-[80vw] h-[90vh]">
            <ViewSourceDialogContent
              source={source || null}
              isOpenAPI={isOpenAPI}
            />
          </Dialog.Content>
        </Dialog>
      )}

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <Dialog.Content className="max-w-2xl!">
          {assetForDialog && (
            <RemoveSourceDialogContent
              asset={assetForDialog}
              onConfirmRemoval={handleRemoveSource}
              onClose={() => setDeleteDialogOpen(false)}
            />
          )}
        </Dialog.Content>
      </Dialog>
    </>
  );
}
