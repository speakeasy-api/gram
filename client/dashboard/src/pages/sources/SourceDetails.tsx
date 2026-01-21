import { Page } from "@/components/page-layout";
import {
  Button,
  Badge,
  Dialog,
  Combobox,
  Icon,
} from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { Heading } from "@/components/ui/heading";
import { useProject } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import {
  useLatestDeployment,
  useListAssets,
  useListEnvironments,
  useGetSourceEnvironment,
  useSetSourceEnvironmentLinkMutation,
  useDeleteSourceEnvironmentLinkMutation,
} from "@gram/client/react-query/index.js";
import { useParams, Navigate } from "react-router";
import { useRoutes } from "@/routes";
import {
  FileCode,
  SquareFunction,
  Download,
  Calendar,
  Eye,
  TriangleAlertIcon,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { useMemo, useState, useEffect } from "react";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { toast } from "sonner";
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

  // Environment management state and hooks
  const environments = useListEnvironments();
  const sourceEnvironment = useGetSourceEnvironment(
    {
      sourceKind: (sourceKind === "openapi" ? "http" : sourceKind) as
        | "http"
        | "function",
      sourceSlug: sourceSlug || "",
    },
    undefined,
    {
      retry: (_, err) => {
        if (err instanceof GramError && err.statusCode === 404) {
          return false;
        }
        return true;
      },
      throwOnError: false,
      enabled: !!sourceKind && !!sourceSlug,
    },
  );

  const [activeEnvironmentId, setActiveEnvironmentId] = useState<
    string | undefined
  >(undefined);

  const [initialEnvironmentId, setInitialEnvironmentId] = useState<
    string | undefined
  >(undefined);

  useEffect(() => {
    setActiveEnvironmentId(sourceEnvironment.data?.id);
    setInitialEnvironmentId(sourceEnvironment.data?.id);
  }, [sourceEnvironment.data?.id]);

  const isDirty = activeEnvironmentId !== initialEnvironmentId;

  const setSourceEnvironmentMutation = useSetSourceEnvironmentLinkMutation({
    onSuccess: () => {
      toast.success("Environment attached successfully");
      setInitialEnvironmentId(activeEnvironmentId);
    },
    onError: (error) => {
      toast.error("Failed to attach environment. Please try again.");
      console.error("Failed to attach environment:", error);
    },
    onSettled: () => {
      sourceEnvironment.refetch();
    },
  });

  const deleteSourceEnvironmentMutation =
    useDeleteSourceEnvironmentLinkMutation({
      onSuccess: () => {
        toast.success("Environment detached successfully");
        setInitialEnvironmentId(undefined);
      },
      onError: (error) => {
        toast.error("Failed to detach environment. Please try again.");
        console.error("Failed to detach environment:", error);
      },
      onSettled: () => {
        sourceEnvironment.refetch();
      },
    });

  const handleSaveEnvironment = () => {
    if (!activeEnvironmentId && isDirty && sourceSlug) {
      deleteSourceEnvironmentMutation.mutate({
        request: {
          sourceKind: (sourceKind === "openapi" ? "http" : sourceKind) as
            | "http"
            | "function",
          sourceSlug: sourceSlug,
        },
      });
      return;
    }

    if (!activeEnvironmentId || !sourceSlug) return;

    setSourceEnvironmentMutation.mutate({
      request: {
        setSourceEnvironmentLinkRequestBody: {
          sourceKind: (sourceKind === "openapi" ? "http" : sourceKind) as
            | "http"
            | "function",
          sourceSlug: sourceSlug,
          environmentId: activeEnvironmentId,
        },
      },
    });
  };

  const selectedEnvironment = environments.data?.environments?.find(
    (env) => env.id === activeEnvironmentId,
  );

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
                  Environment
                </Type>
                {selectedEnvironment ? (
                  <routes.environments.environment.Link
                    params={[selectedEnvironment.slug]}
                    className="font-medium hover:underline"
                  >
                    {selectedEnvironment.name}
                  </routes.environments.environment.Link>
                ) : (
                  <Type className="font-medium">Unattached</Type>
                )}
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

          {/* Attached Environment Section */}
          <div className="rounded-lg border bg-card p-6">
            <Type as="h2" className="text-lg font-semibold mb-4">
              Attached Environment
            </Type>
            <div className="space-y-4">
              <div className="space-y-2">
                <p className="text-warning text-sm flex items-center gap-2">
                  <TriangleAlertIcon className="w-4 h-4" />
                  Environments attached here will apply to all users of tools
                  from this source in both public and private servers
                </p>
                {isOpenAPI ? (
                  <p className="text-sm text-muted-foreground flex items-center gap-1.5">
                    Values set here will be forwarded to{" "}
                    <span className="inline-flex items-center gap-1 bg-secondary px-1.5 py-0.5 rounded">
                      <FileCode className="w-3 h-3" /> {source?.name}
                    </span>
                  </p>
                ) : (
                  <p className="text-sm text-muted-foreground flex items-center gap-1.5">
                    You will be able to access values set here on{" "}
                    <code className="text-xs bg-muted px-1 py-0.5 rounded">
                      process.env
                    </code>{" "}
                    in{" "}
                    <span className="inline-flex items-center gap-1 bg-secondary px-1.5 py-0.5 rounded">
                      <SquareFunction className="w-3 h-3" /> {source?.name}
                    </span>
                  </p>
                )}
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <p className="text-sm font-medium">Environment</p>
                  {selectedEnvironment ? (
                    <routes.environments.environment.Link
                      params={[selectedEnvironment.slug]}
                    >
                      <Button
                        variant="tertiary"
                        size="sm"
                        aria-label="View environment"
                      >
                        <Icon name="eye" /> view
                      </Button>
                    </routes.environments.environment.Link>
                  ) : (
                    <Button
                      variant="tertiary"
                      size="sm"
                      aria-label="View environment"
                      disabled
                    >
                      <Icon name="eye" /> view
                    </Button>
                  )}
                </div>
                <div className="flex gap-2 items-center w-full">
                  <div className="flex-1">
                    <Combobox
                      value={activeEnvironmentId ?? ""}
                      placeholder="select environment"
                      options={(environments.data?.environments ?? []).map(
                        (env) => ({
                          value: env.id,
                          label: env.name,
                        }),
                      )}
                      onValueChange={setActiveEnvironmentId}
                      loading={
                        environments.isLoading || sourceEnvironment.isLoading
                      }
                    />
                  </div>
                  {activeEnvironmentId && (
                    <Button
                      onClick={() => setActiveEnvironmentId(undefined)}
                      variant="tertiary"
                      size="sm"
                      aria-label="Clear environment"
                    >
                      <Icon name="x" /> clear
                    </Button>
                  )}
                </div>
              </div>

              <div className="space-y-2 min-h-10">
                {selectedEnvironment && (
                  <div className="flex flex-wrap gap-2 items-center">
                    {selectedEnvironment.entries.length > 0 ? (
                      selectedEnvironment.entries.map((entry) => (
                        <Badge key={entry.name}>{entry.name}</Badge>
                      ))
                    ) : (
                      <div className="text-sm text-muted-foreground">
                        Empty...
                      </div>
                    )}
                  </div>
                )}
              </div>

              {isDirty && (
                <div className="flex gap-2 justify-end pt-2 border-t">
                  <Button
                    onClick={() => setActiveEnvironmentId(initialEnvironmentId)}
                    variant="secondary"
                    size="sm"
                  >
                    Cancel
                  </Button>
                  <Button
                    onClick={handleSaveEnvironment}
                    variant="primary"
                    size="sm"
                    disabled={
                      setSourceEnvironmentMutation.isPending ||
                      deleteSourceEnvironmentMutation.isPending
                    }
                  >
                    Save Changes
                  </Button>
                </div>
              )}
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
