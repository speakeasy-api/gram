import { Page } from "@/components/page-layout";
import {
  Button,
  Badge,
  Dialog,
  Combobox,
  Icon,
} from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { CodeBlock } from "@/components/code";
import { SkeletonCode } from "@/components/ui/skeleton";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import {
  useLatestDeployment,
  useListAssets,
  useListTools,
  useListToolsets,
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
  Package,
  Eye,
  TriangleAlertIcon,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { useMemo, useState, useCallback, useEffect } from "react";
import { unzipSync, strFromU8 } from "fflate";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { toast } from "sonner";

export default function SourceDetails() {
  const { sourceKind, sourceSlug } = useParams<{
    sourceKind: string;
    sourceSlug: string;
  }>();
  const routes = useRoutes();
  const project = useProject();
  const { projectSlug } = useSlugs();
  const { data: deployment, isLoading: isLoadingDeployment } =
    useLatestDeployment();
  const { data: assetsData } = useListAssets();
  const { data: toolsData } = useListTools({
    deploymentId: deployment?.deployment?.id,
  });
  const { data: toolsetsData } = useListToolsets();

  const [sourceContent, setSourceContent] = useState<string>("");
  const [isLoadingContent, setIsLoadingContent] = useState(false);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [fetchError, setFetchError] = useState<string | null>(null);

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
  const SourceIcon = isOpenAPI ? FileCode : SquareFunction;
  const sourceType = isOpenAPI ? "OpenAPI" : "Function";

  // Get tools generated by this source
  // The API returns polymorphic Tool objects with httpToolDefinition or functionToolDefinition
  const sourceTools = useMemo(() => {
    if (!source || !toolsData) {
      return [];
    }

    const isOpenAPISource = sourceKind === "http" || sourceKind === "openapi";

    // Extract HTTP tools or function tools based on source type
    if (isOpenAPISource) {
      return toolsData.tools
        .filter((tool) => tool.httpToolDefinition?.assetId === source.assetId)
        .map((tool) => tool.httpToolDefinition!);
    } else {
      return toolsData.tools
        .filter(
          (tool) => tool.functionToolDefinition?.assetId === source.assetId,
        )
        .map((tool) => tool.functionToolDefinition!);
    }
  }, [source, toolsData, sourceKind]);

  // Get tool URNs for this source
  const sourceToolUrns = useMemo(() => {
    return new Set(sourceTools.map((tool) => tool.toolUrn));
  }, [sourceTools]);

  // Find toolsets that use tools from this source
  const toolsetsUsingSource = useMemo(() => {
    if (!toolsetsData || sourceToolUrns.size === 0) return [];

    return toolsetsData.toolsets.filter((toolset) =>
      toolset.toolUrns?.some((urn) => sourceToolUrns.has(urn)),
    );
  }, [toolsetsData, sourceToolUrns]);

  // Fetch source content using SDK client's httpClient for proper auth handling
  const fetchContent = useCallback(async () => {
    if (!source) return;

    setIsLoadingContent(true);
    setFetchError(null);
    try {
      const path = isOpenAPI
        ? "/rpc/assets.serveOpenAPIv3"
        : "/rpc/assets.serveFunction";
      const url = new URL(path, getServerURL());
      url.searchParams.set("id", source.assetId);
      url.searchParams.set("project_id", project.id);

      // Use the same fetch pattern as the SDK client
      const request = new Request(url.toString(), {
        method: "GET",
        credentials: "include",
      });

      // Add the gram-project header like the SDK does
      if (projectSlug) {
        request.headers.set("gram-project", projectSlug);
      }

      const response = await fetch(request);

      if (response.ok) {
        if (isOpenAPI) {
          // OpenAPI specs are served as text (YAML/JSON)
          const text = await response.text();
          setSourceContent(text);
        } else {
          // Function bundles are served as zip files - extract manifest.json
          const arrayBuffer = await response.arrayBuffer();
          const uint8Array = new Uint8Array(arrayBuffer);
          const unzipped = unzipSync(uint8Array);

          // Try to extract manifest.json (contains tool definitions)
          if (unzipped["manifest.json"]) {
            const manifestText = strFromU8(unzipped["manifest.json"]);
            const manifest = JSON.parse(manifestText);
            // Pretty-print the manifest
            setSourceContent(JSON.stringify(manifest, null, 2));
          } else if (unzipped["functions.js"]) {
            // Fallback to functions.js if no manifest
            const code = strFromU8(unzipped["functions.js"]);
            setSourceContent(code);
          } else {
            setFetchError("No readable content found in bundle");
          }
        }
      } else {
        setFetchError(
          `Failed to load: ${response.status} ${response.statusText}`,
        );
      }
    } catch (error) {
      setFetchError(
        error instanceof Error ? error.message : "Failed to fetch content",
      );
    } finally {
      setIsLoadingContent(false);
    }
  }, [source, project.id, isOpenAPI, projectSlug]);

  // Open modal and fetch content
  const handleViewSpec = useCallback(() => {
    setIsModalOpen(true);
    if (!sourceContent && !isLoadingContent) {
      fetchContent();
    }
  }, [sourceContent, isLoadingContent, fetchContent]);

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
        />
      </Page.Header>

      <Page.Body>
        {/* Header Section with Title and Actions */}
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-surface-secondary">
              <SourceIcon className="h-5 w-5 text-muted-foreground" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-semibold">
                  {source?.name || sourceSlug}
                </h1>
                <Badge variant="outline">{sourceType}</Badge>
              </div>
              <Type className="text-muted-foreground text-sm" as="p">
                {source?.slug}
              </Type>
            </div>
          </div>
          <div className="flex gap-2">
            <Button
              variant="secondary"
              size="sm"
              icon={<Eye />}
              onClick={handleViewSpec}
            >
              View {isOpenAPI ? "Spec" : "Manifest"}
            </Button>
            <Button
              variant="secondary"
              size="sm"
              icon={<Download />}
              onClick={handleDownload}
            >
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
                <Type className="font-mono text-sm">{source?.slug}</Type>
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
                  <Type className="font-mono text-sm">{source.runtime}</Type>
                </div>
              )}
              <div>
                <Type className="text-sm text-muted-foreground mb-1">
                  Last Updated
                </Type>
                <div className="flex items-center gap-2">
                  <Calendar className="h-4 w-4 text-muted-foreground" />
                  <Type className="text-sm">{lastUpdated}</Type>
                </div>
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

          {/* Toolsets Using This Source */}
          <div className="rounded-lg border bg-card p-6">
            <Type as="h2" className="text-lg font-semibold mb-4">
              Used in Toolsets ({toolsetsUsingSource.length})
            </Type>
            {toolsetsUsingSource.length === 0 ? (
              <div className="text-center py-8 text-muted-foreground">
                <Package className="h-12 w-12 mx-auto mb-3 opacity-50" />
                <Type>This source is not currently used in any toolsets</Type>
              </div>
            ) : (
              <div className="space-y-2">
                {toolsetsUsingSource.map((toolset) => (
                  <div
                    key={toolset.slug}
                    onClick={() => routes.toolsets.toolset.goTo(toolset.slug)}
                    className="flex items-center justify-between p-3 rounded-md border bg-surface-secondary hover:bg-surface-tertiary transition-colors cursor-pointer"
                  >
                    <div className="flex-1">
                      <Type className="font-medium">{toolset.name}</Type>
                      {toolset.description && (
                        <Type className="text-sm text-muted-foreground mt-1">
                          {toolset.description}
                        </Type>
                      )}
                    </div>
                    <Badge variant="outline" className="ml-2">
                      {
                        toolset.toolUrns?.filter((urn) =>
                          sourceToolUrns.has(urn),
                        ).length
                      }{" "}
                      tools
                    </Badge>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* View Spec/Source Modal */}
        <Dialog open={isModalOpen} onOpenChange={setIsModalOpen}>
          <Dialog.Content className="min-w-[80vw] h-[90vh]">
            <Dialog.Header>
              <Dialog.Title>
                {source?.name} -{" "}
                {isOpenAPI ? "OpenAPI Specification" : "Tool Manifest"}
              </Dialog.Title>
              {!isOpenAPI && (
                <Type className="text-muted-foreground text-sm mt-1">
                  Shows the tool definitions extracted from the function bundle
                </Type>
              )}
            </Dialog.Header>
            <div className="flex-1 overflow-auto">
              {isLoadingContent ? (
                <SkeletonCode lines={20} />
              ) : fetchError ? (
                <div className="text-center py-8">
                  <Type className="text-destructive">{fetchError}</Type>
                  <Button
                    variant="secondary"
                    size="sm"
                    className="mt-4"
                    onClick={fetchContent}
                  >
                    Retry
                  </Button>
                </div>
              ) : sourceContent ? (
                <CodeBlock language={isOpenAPI ? "yaml" : "json"} copyable>
                  {sourceContent}
                </CodeBlock>
              ) : (
                <Type className="text-muted-foreground text-center py-8">
                  No content available
                </Type>
              )}
            </div>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}
