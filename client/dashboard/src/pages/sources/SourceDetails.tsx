import { Page } from "@/components/page-layout";
import { Button, Badge, Dialog } from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { Heading } from "@/components/ui/heading";
import { useProject } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import {
  useLatestDeployment,
  useListAssets,
} from "@gram/client/react-query/index.js";
import { useParams, Navigate } from "react-router";
import { useRoutes } from "@/routes";
import { Download, Calendar, Eye } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { useMemo, useState } from "react";
import { ViewSourceDialogContent } from "@/components/sources/ViewSourceDialogContent";
import ExternalMCPDetails from "./external-mcp/ExternalMCPDetails";

export default function SourceDetails() {
  const { sourceKind, sourceSlug } = useParams<{
    sourceKind: string;
    sourceSlug: string;
  }>();
  const routes = useRoutes();
  const project = useProject();
  const { data: deployment, isLoading: isLoadingDeployment } =
    useLatestDeployment();
  const { data: assetsData } = useListAssets();

  const [isModalOpen, setIsModalOpen] = useState(false);

  // Find the specific source from the deployment
  const source = useMemo(() => {
    if (!deployment?.deployment) return null;

    if (sourceKind === "http" || sourceKind === "openapi") {
      return deployment.deployment.openapiv3Assets?.find(
        (asset) => asset.slug === sourceSlug,
      );
    } else if (sourceKind === "function") {
      return deployment.deployment.functionsAssets?.find(
        (func) => func.slug === sourceSlug,
      );
    }
    return null;
  }, [deployment, sourceKind, sourceSlug]);

  // Get the underlying Asset (which has updatedAt) by looking up via assetId
  const underlyingAsset = useMemo(() => {
    if (!source || !assetsData) return null;
    return assetsData.assets.find((a) => a.id === source.assetId);
  }, [source, assetsData]);

  const isOpenAPI = sourceKind === "http" || sourceKind === "openapi";
  const sourceType = isOpenAPI ? "OpenAPI" : "Function";

  // Download functionality
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

  // Redirect to ExternalMCPDetails for external MCP servers
  if (sourceKind === "externalmcp") {
    return <ExternalMCPDetails />;
  }

  // If source not found, redirect to home
  if (!isLoadingDeployment && !source) {
    return <Navigate to={routes.home.href()} replace />;
  }

  // Format the updated date from the underlying Asset
  const lastUpdated = underlyingAsset?.updatedAt
    ? formatDistanceToNow(new Date(underlyingAsset.updatedAt), {
        addSuffix: true,
      })
    : "Unknown";

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [sourceSlug || ""]: source?.name }}
          skipSegments={[sourceKind || ""]}
        />
      </Page.Header>

      <Page.Body>
        {/* Header Section with Title and Actions */}
        <div className="flex items-center justify-between mb-4 h-10">
          <div className="flex items-center gap-2">
            <Heading variant="h2" className="normal-case">
              {source?.name || sourceSlug}
            </Heading>
            <Badge variant="neutral">{sourceType}</Badge>
          </div>
          <div className="flex gap-2">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setIsModalOpen(true)}
            >
              <Eye className="h-4 w-4" />
              View {isOpenAPI ? "Spec" : "Manifest"}
            </Button>
            <Button variant="secondary" size="sm" onClick={handleDownload}>
              <Download className="h-4 w-4" />
              Download
            </Button>
          </div>
        </div>

        <div className="space-y-6">
          {/* Source Metadata Card */}
          <div className="rounded-lg border bg-card p-6">
            <Type as="h2" className="text-lg font-semibold mb-4">
              Source Information
            </Type>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <Type className="text-sm text-muted-foreground mb-1">Name</Type>
                <Type className="font-medium">{source?.name}</Type>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">Slug</Type>
                <Type className="font-medium">{source?.slug}</Type>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">Type</Type>
                <Type className="font-medium">{sourceType}</Type>
              </div>
              {!isOpenAPI && source && "runtime" in source && (
                <div>
                  <Type className="text-sm text-muted-foreground mb-1">
                    Runtime
                  </Type>
                  <Type className="font-medium">{String(source.runtime)}</Type>
                </div>
              )}
              <div>
                <Type className="text-sm text-muted-foreground mb-1">
                  Last Updated
                </Type>
                <div className="flex items-center gap-2">
                  <Calendar className="h-4 w-4 text-muted-foreground" />
                  <Type className="font-medium">{lastUpdated}</Type>
                </div>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">
                  Deployment
                </Type>
                {deployment?.deployment?.id ? (
                  <routes.deployments.deployment.Link
                    params={[deployment.deployment.id]}
                    className="font-medium hover:underline"
                  >
                    {deployment.deployment.id.slice(0, 8)}
                  </routes.deployments.deployment.Link>
                ) : (
                  <Type className="font-medium">None</Type>
                )}
              </div>
            </div>
          </div>
        </div>

        {/* View Spec/Source Modal */}
        <Dialog open={isModalOpen} onOpenChange={setIsModalOpen}>
          <Dialog.Content className="min-w-[80vw] h-[90vh]">
            <ViewSourceDialogContent
              source={source || null}
              isOpenAPI={isOpenAPI}
            />
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}
