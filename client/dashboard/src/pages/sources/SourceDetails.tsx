import { Page } from "@/components/page-layout";
import { Button, Badge, Dialog, Stack } from "@speakeasy-api/moonshine";
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
import { Download, Eye, FileCode, Tag, Package, Clock } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { useMemo, useState } from "react";
import { ViewSourceDialogContent } from "@/components/sources/ViewSourceDialogContent";
import ExternalMCPDetails from "./external-mcp/ExternalMCPDetails";
import {
  OpenAPIIllustration,
  FunctionIllustration,
} from "@/components/sources/SourceCardIllustrations";
import { InfoField } from "@/components/sources/InfoField";

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

      <Page.Body fullWidth noPadding>
        {/* Hero Header with Illustration - full width */}
        <div className="relative w-full h-64 overflow-hidden">
          {isOpenAPI ? (
            <OpenAPIIllustration className="saturate-[.3]" />
          ) : (
            <FunctionIllustration className="saturate-[.3]" />
          )}

          {/* Overlay for text readability */}
          <div className="absolute inset-0 bg-linear-to-t from-foreground/50 via-foreground/20 to-transparent" />
          <div className="absolute bottom-0 left-0 right-0 px-8 py-8 max-w-[1270px] mx-auto w-full">
            <Stack gap={2}>
              <div className="flex items-center gap-3 ml-1">
                <Heading variant="h1" className="text-background">
                  {source?.name || sourceSlug}
                </Heading>
                <Badge variant="neutral">
                  <Badge.Text>{sourceType}</Badge.Text>
                </Badge>
              </div>
              <div className="flex items-center gap-2 ml-1">
                <Type className="max-w-2xl truncate text-background/70!">
                  {source?.slug}
                </Type>
              </div>
            </Stack>
          </div>

          {/* Action buttons */}
          <div className="absolute top-6 left-0 right-0 px-8 max-w-[1270px] mx-auto w-full">
            <Stack direction="horizontal" gap={2} className="justify-end">
              <Button
                variant="secondary"
                size="md"
                onClick={() => setIsModalOpen(true)}
              >
                <Button.LeftIcon>
                  <Eye className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>View {isOpenAPI ? "Spec" : "Manifest"}</Button.Text>
              </Button>
              <Button variant="secondary" size="md" onClick={handleDownload}>
                <Button.LeftIcon>
                  <Download className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Download</Button.Text>
              </Button>
            </Stack>
          </div>
        </div>

        {/* Content Section */}
        <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
          <div className="space-y-6">
          {/* Source Metadata Card */}
          <div className="rounded-lg border bg-card overflow-hidden">
            <div className="border-b bg-surface-secondary/30 px-6 py-4">
              <Type as="h2" className="text-lg flex items-center gap-2">
                <FileCode className="h-5 w-5 text-muted-foreground" />
                Source Information
              </Type>
            </div>
            <div className="p-6">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <InfoField
                  icon={Tag}
                  label="Name"
                  value={source?.name}
                />

                <InfoField
                  icon={Tag}
                  label="Slug"
                  value={<Type className="font-mono text-sm">{source?.slug}</Type>}
                />

                <InfoField
                  icon={Package}
                  label="Type"
                  value={sourceType}
                />

                {!isOpenAPI && source && "runtime" in source && (
                  <InfoField
                    icon={Package}
                    label="Runtime"
                    value={String(source.runtime)}
                  />
                )}

                <InfoField
                  icon={Clock}
                  label="Last Updated"
                  value={lastUpdated}
                />

                <InfoField
                  icon={Package}
                  label="Deployment"
                  value={
                    deployment?.deployment?.id ? (
                      <routes.deployments.deployment.Link
                        params={[deployment.deployment.id]}
                        className="hover:underline text-primary"
                      >
                        {deployment.deployment.id.slice(0, 8)}
                      </routes.deployments.deployment.Link>
                    ) : (
                      <Type className="text-muted-foreground">None</Type>
                    )
                  }
                />
              </div>
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
