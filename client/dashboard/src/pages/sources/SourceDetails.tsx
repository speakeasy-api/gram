import MonacoEditorLazy from "@/components/monaco-editor.lazy";
import { Page } from "@/components/page-layout";
import { RemoveSourceDialogContent } from "@/components/sources/RemoveSourceDialogContent";
import { MCPPatternIllustration } from "@/components/sources/SourceCardIllustrations";
import {
  useFetchSourceContent,
  ViewSourceDialogContent,
} from "@/components/sources/ViewSourceDialogContent";
import { Heading } from "@/components/ui/heading";
import { SkeletonCode } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  useLatestDeployment,
  useListAssets,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { ToolsetEntry } from "@gram/client/models/components";
import { useListTools } from "@/hooks/toolTypes";
import { Badge, Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { format, formatDistanceToNow } from "date-fns";
import {
  ChevronRight,
  Download,
  Eye,
  Globe,
  Lock,
  Power,
  Search,
  Server,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Navigate, useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import ExternalMCPDetails from "./external-mcp/ExternalMCPDetails";

export default function SourceDetails() {
  const { sourceKind, sourceSlug } = useParams<{
    sourceKind: string;
    sourceSlug: string;
  }>();
  const routes = useRoutes();
  const navigate = useNavigate();
  const project = useProject();
  const client = useSdkClient();
  const {
    data: deployment,
    isLoading: isLoadingDeployment,
    refetch,
  } = useLatestDeployment();
  const { data: assetsData, refetch: refetchAssets } = useListAssets();

  const { projectSlug } = useSlugs();
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [methodFilter, setMethodFilter] = useState<string | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");

  // Tab state from URL hash - initialized to hash or "overview"
  const [activeTab, setActiveTab] = useState(() => {
    const hash = window.location.hash.replace("#", "");
    // Initial validation will be done after we know which tabs are valid
    return hash || "overview";
  });

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

  // Get tools derived from this source
  const { data: toolsData } = useListTools(
    { deploymentId: deployment?.deployment?.id },
    undefined,
    { enabled: !!deployment?.deployment?.id },
  );

  const relatedTools = useMemo(() => {
    if (!toolsData?.tools || !source) return [];
    return toolsData.tools.filter(
      (tool) => tool.type === "http" && tool.openapiv3DocumentId === source.id,
    );
  }, [toolsData, source]);

  // Get toolsets to find which MCP servers use this source
  const { data: toolsetsData } = useListToolsets();

  // Find toolsets that contain tools from this source
  const associatedToolsets = useMemo(() => {
    if (!toolsetsData?.toolsets || !relatedTools.length) return [];

    // Get all tool URNs from this source
    const sourceToolUrns = new Set(relatedTools.map((t) => t.toolUrn));

    // Find toolsets that have any of these tool URNs
    return toolsetsData.toolsets.filter((toolset) =>
      toolset.toolUrns?.some((urn) => sourceToolUrns.has(urn)),
    );
  }, [toolsetsData, relatedTools]);

  const isOpenAPI = sourceKind === "http" || sourceKind === "openapi";
  const sourceType = isOpenAPI ? "OpenAPI" : "Function";

  // Build valid tabs dynamically based on source type and associated toolsets
  const validTabs = useMemo(() => {
    const tabs = ["overview", "tools"];
    if (associatedToolsets.length > 0) {
      tabs.push("mcp-servers");
    }
    if (isOpenAPI) {
      tabs.push("spec");
    }
    tabs.push("settings");
    return tabs;
  }, [isOpenAPI, associatedToolsets.length]);

  // Validate and correct activeTab when validTabs changes
  useEffect(() => {
    if (!validTabs.includes(activeTab)) {
      setActiveTab("overview");
      window.location.hash = "overview";
    }
  }, [validTabs, activeTab]);

  // Listen for hash changes
  useEffect(() => {
    const handleHashChange = () => {
      const hash = window.location.hash.replace("#", "");
      if (validTabs.includes(hash)) {
        setActiveTab(hash);
      }
    };
    window.addEventListener("hashchange", handleHashChange);
    return () => window.removeEventListener("hashchange", handleHashChange);
  }, [validTabs]);

  const handleTabChange = (value: string) => {
    setActiveTab(value);
    window.location.hash = value;
  };

  // Fetch spec content for OpenAPI sources
  const {
    data: specContent,
    isLoading: isLoadingSpec,
    error: specError,
    refetch: refetchSpec,
  } = useFetchSourceContent(source, isOpenAPI, project, projectSlug);

  // Format file size
  const formatFileSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

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

  const handleRemoveSource = async (
    assetId: string,
    type: "openapi" | "function" | "externalmcp",
  ) => {
    try {
      await client.deployments.create({
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

  // Create asset object for delete dialog
  const assetForDialog = source
    ? {
        id: source.assetId,
        deploymentAssetId: source.id,
        name: source.name,
        slug: source.slug,
        type: isOpenAPI ? ("openapi" as const) : ("function" as const),
      }
    : null;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [sourceSlug || ""]: source?.name }}
          skipSegments={[sourceKind || ""]}
        />
      </Page.Header>

      <Page.Body fullWidth noPadding fullHeight overflowHidden>
        {/* Hero Header with Illustration - full width */}
        <div className="relative w-full h-64 shrink-0 overflow-hidden">
          <MCPPatternIllustration
            toolsetSlug={sourceSlug || ""}
            className="saturate-[.3]"
          />

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
        </div>

        {/* Tabs Navigation */}
        <Tabs
          value={activeTab}
          onValueChange={handleTabChange}
          className="w-full flex-1 flex flex-col min-h-0"
        >
          <div className="border-b shrink-0">
            <div className="max-w-[1270px] mx-auto px-8">
              <TabsList className="h-auto bg-transparent p-0 gap-6 rounded-none">
                <TabsTrigger
                  value="overview"
                  className="relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none! text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent! after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary"
                >
                  Overview
                </TabsTrigger>
                <TabsTrigger
                  value="tools"
                  className="relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none! text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent! after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary"
                >
                  Tools {relatedTools.length > 0 && `(${relatedTools.length})`}
                </TabsTrigger>
                {associatedToolsets.length > 0 && (
                  <TabsTrigger
                    value="mcp-servers"
                    className="relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none! text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent! after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary"
                  >
                    MCP Servers ({associatedToolsets.length})
                  </TabsTrigger>
                )}
                {isOpenAPI && (
                  <TabsTrigger
                    value="spec"
                    className="relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none! text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent! after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary"
                  >
                    OpenAPI Specification
                  </TabsTrigger>
                )}
                <TabsTrigger
                  value="settings"
                  className="relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none! text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent! after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary"
                >
                  Settings
                </TabsTrigger>
              </TabsList>
            </div>
          </div>

          {/* Overview Tab */}
          <TabsContent value="overview" className="mt-0 flex-1">
            <div className="max-w-[1270px] mx-auto px-8 py-8 w-full space-y-6">
              {/* Row 1: Name, Format, File Size (or Runtime for functions) */}
              <div className="flex gap-16">
                <div>
                  <Type muted small className="mb-1">
                    Name
                  </Type>
                  <Type className="font-medium">{source?.name || "—"}</Type>
                </div>
                {isOpenAPI ? (
                  <div>
                    <Type muted small className="mb-1">
                      Format
                    </Type>
                    <Type className="font-mono">
                      {underlyingAsset?.contentType?.includes("yaml")
                        ? "YAML"
                        : underlyingAsset?.contentType?.includes("json")
                          ? "JSON"
                          : underlyingAsset?.contentType || "—"}
                    </Type>
                  </div>
                ) : (
                  <div>
                    <Type muted small className="mb-1">
                      Runtime
                    </Type>
                    <Type>
                      {source && "runtime" in source
                        ? String(source.runtime)
                        : "—"}
                    </Type>
                  </div>
                )}
                <div>
                  <Type muted small className="mb-1">
                    File Size
                  </Type>
                  <Type>
                    {underlyingAsset?.contentLength
                      ? formatFileSize(underlyingAsset.contentLength)
                      : "—"}
                  </Type>
                </div>
              </div>

              {/* Row 2: Last Updated, Created At */}
              <div className="flex gap-16">
                <div>
                  <Type muted small className="mb-1">
                    Last Updated
                  </Type>
                  <Type>{lastUpdated}</Type>
                </div>
                <div>
                  <Type muted small className="mb-1">
                    Created
                  </Type>
                  <Type>
                    {underlyingAsset?.createdAt
                      ? format(
                          new Date(underlyingAsset.createdAt),
                          "MMM d, yyyy",
                        )
                      : "—"}
                  </Type>
                </div>
              </div>

              {/* Row 3: Deployment */}
              <div>
                <Type muted small className="mb-1">
                  Deployment
                </Type>
                {deployment?.deployment?.id ? (
                  <routes.deployments.deployment.Link
                    params={[deployment.deployment.id]}
                    className="hover:underline text-primary font-mono"
                  >
                    {deployment.deployment.id.slice(0, 8)}
                  </routes.deployments.deployment.Link>
                ) : (
                  <Type className="text-muted-foreground">None</Type>
                )}
              </div>
            </div>
          </TabsContent>

          {/* Tools Tab */}
          <TabsContent
            value="tools"
            className="mt-0 flex-1 flex flex-col min-h-0"
          >
            <div className="max-w-[1270px] mx-auto px-8 py-6 w-full flex-1 flex flex-col min-h-0">
              {relatedTools.length > 0 ? (
                <div className="flex flex-col gap-4 flex-1 min-h-0">
                  {/* Method filter pills */}
                  <div className="flex gap-2 flex-wrap shrink-0">
                    <button onClick={() => setMethodFilter(null)}>
                      <Badge
                        variant={methodFilter === null ? "default" : "neutral"}
                        className="py-2"
                      >
                        <Badge.Text>
                          All (
                          {relatedTools.filter((t) => t.type === "http").length}
                          )
                        </Badge.Text>
                      </Badge>
                    </button>
                    {["GET", "POST", "PUT", "PATCH", "DELETE"].map((method) => {
                      const count = relatedTools.filter(
                        (t) => t.type === "http" && t.httpMethod === method,
                      ).length;
                      if (count === 0) return null;
                      const isActive = methodFilter === method;
                      const variant =
                        method === "GET"
                          ? "success"
                          : method === "POST"
                            ? "information"
                            : method === "PUT"
                              ? "warning"
                              : method === "PATCH"
                                ? "neutral"
                                : "destructive";
                      return (
                        <button
                          key={method}
                          onClick={() =>
                            setMethodFilter(isActive ? null : method)
                          }
                        >
                          <Badge
                            variant={variant}
                            className={`py-2 ${isActive ? "" : "opacity-50 hover:opacity-100"}`}
                          >
                            <Badge.Text>
                              {method} ({count})
                            </Badge.Text>
                          </Badge>
                        </button>
                      );
                    })}
                  </div>

                  {/* Tools table */}
                  <div className="border rounded-lg flex flex-col overflow-hidden flex-1 min-h-0 mb-4">
                    {/* Fixed header */}
                    <div className="border-b bg-muted/50 shrink-0">
                      <div className="grid grid-cols-[80px_40%_1fr] items-center px-4 py-1">
                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                          Method
                        </div>
                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider pr-3">
                          Endpoint
                        </div>
                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider flex items-center justify-between">
                          <span>Tool Name</span>
                          <div className="flex items-center">
                            <div
                              className={`flex items-center overflow-hidden transition-all duration-200 ${
                                searchOpen ? "w-48 mr-2" : "w-0"
                              }`}
                            >
                              <input
                                type="text"
                                placeholder="Search tools..."
                                value={searchQuery}
                                onChange={(e) => setSearchQuery(e.target.value)}
                                onBlur={() => {
                                  if (!searchQuery) {
                                    setSearchOpen(false);
                                  }
                                }}
                                className="w-full px-2 py-1 text-sm font-normal normal-case tracking-normal border border-border rounded bg-background focus:outline-none focus:border-muted-foreground"
                                autoFocus={searchOpen}
                              />
                            </div>
                            <button
                              onClick={() => {
                                if (searchOpen && searchQuery) {
                                  setSearchQuery("");
                                } else {
                                  setSearchOpen(!searchOpen);
                                }
                              }}
                              className="p-1 rounded hover:bg-muted transition-colors"
                            >
                              {searchOpen ? (
                                <X className="h-4 w-4" />
                              ) : (
                                <Search className="h-4 w-4" />
                              )}
                            </button>
                          </div>
                        </div>
                      </div>
                    </div>
                    {/* Scrollable body */}
                    <div className="flex-1 overflow-y-auto">
                      {(() => {
                        const filteredTools = relatedTools.filter((tool) => {
                          if (tool.type !== "http") return false;
                          if (methodFilter && tool.httpMethod !== methodFilter)
                            return false;
                          if (searchQuery) {
                            const query = searchQuery.toLowerCase();
                            return (
                              tool.name.toLowerCase().includes(query) ||
                              tool.path.toLowerCase().includes(query)
                            );
                          }
                          return true;
                        });

                        if (filteredTools.length === 0) {
                          return (
                            <div className="flex items-center justify-center h-full">
                              <Type muted>No matching tools found</Type>
                            </div>
                          );
                        }

                        return filteredTools.map((tool) => {
                          if (tool.type !== "http") return null;
                          return (
                            <div
                              key={tool.toolUrn}
                              className="grid grid-cols-[80px_40%_1fr] items-center px-4 py-3 border-b last:border-b-0 hover:bg-muted/30 transition-colors"
                            >
                              <div>
                                <Badge
                                  variant={
                                    tool.httpMethod === "GET"
                                      ? "success"
                                      : tool.httpMethod === "POST"
                                        ? "information"
                                        : tool.httpMethod === "PUT"
                                          ? "warning"
                                          : tool.httpMethod === "PATCH"
                                            ? "neutral"
                                            : "destructive"
                                  }
                                >
                                  <Badge.Text>{tool.httpMethod}</Badge.Text>
                                </Badge>
                              </div>
                              <div className="font-mono text-sm text-muted-foreground truncate pr-3">
                                {tool.path}
                              </div>
                              <div className="text-sm truncate">
                                {tool.name}
                              </div>
                            </div>
                          );
                        });
                      })()}
                    </div>
                  </div>
                </div>
              ) : (
                <div className="text-center py-12">
                  <Type muted>No tools derived from this source yet.</Type>
                </div>
              )}
            </div>
          </TabsContent>

          {/* MCP Servers Tab */}
          {associatedToolsets.length > 0 && (
            <TabsContent value="mcp-servers" className="mt-0 flex-1">
              <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
                <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                  {associatedToolsets.map((toolset) => (
                    <MCPServerPortalCard key={toolset.slug} toolset={toolset} />
                  ))}
                </div>
              </div>
            </TabsContent>
          )}

          {/* Spec Tab (OpenAPI only) */}
          {isOpenAPI && (
            <TabsContent value="spec" className="mt-0">
              {isLoadingSpec ? (
                <div className="p-8">
                  <SkeletonCode lines={20} />
                </div>
              ) : specError ? (
                <div className="text-center py-8">
                  <Type className="text-destructive">
                    {specError instanceof Error
                      ? specError.message
                      : "Failed to fetch spec"}
                  </Type>
                  <Button
                    variant="secondary"
                    size="sm"
                    className="mt-4"
                    onClick={() => refetchSpec()}
                  >
                    <Button.Text>Retry</Button.Text>
                  </Button>
                </div>
              ) : specContent ? (
                <MonacoEditorLazy
                  value={specContent.content}
                  language={specContent.language}
                  height="calc(100vh - 380px)"
                  wordWrap="on"
                />
              ) : (
                <Type className="text-muted-foreground text-center py-8">
                  No spec content available
                </Type>
              )}
            </TabsContent>
          )}

          {/* Settings Tab */}
          <TabsContent value="settings" className="mt-0 flex-1">
            <div className="max-w-[1270px] mx-auto px-8 py-8 w-full space-y-8">
              {/* Source Actions */}
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
                  <Button
                    variant="secondary"
                    size="md"
                    onClick={handleDownload}
                  >
                    <Button.LeftIcon>
                      <Download className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Download</Button.Text>
                  </Button>
                </Stack>
              </div>

              {/* Danger Zone */}
              <div className="border border-destructive/30 rounded-lg p-6">
                <Type variant="subheading" className="text-destructive mb-1">
                  Danger Zone
                </Type>
                <Type muted small className="mb-4">
                  Removing this source will remove it from the current
                  deployment. This action cannot be undone.
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
          </TabsContent>
        </Tabs>

        {/* View Manifest Modal (Function sources only) */}
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

        {/* Delete Dialog */}
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
      </Page.Body>
    </Page>
  );
}

// Portal-style card for MCP servers
function MCPServerPortalCard({ toolset }: { toolset: ToolsetEntry }) {
  const routes = useRoutes();

  return (
    <routes.mcp.details.Link
      params={[toolset.slug]}
      className="group block rounded-xl border bg-card hover:bg-surface-secondary hover:border-primary/30 transition-all duration-200 cursor-pointer hover:no-underline hover:shadow-lg"
    >
      <div className="p-5">
        {/* Header with icon */}
        <div className="flex items-start justify-between gap-3 mb-3">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-primary/10 flex items-center justify-center">
              <Server className="h-5 w-5 text-primary" />
            </div>
            <div>
              <Type className="font-semibold text-base group-hover:text-primary transition-colors">
                {toolset.name}
              </Type>
              <div className="flex items-center gap-2 mt-1">
                <McpEnabledBadge enabled={!!toolset.mcpEnabled} />
                <McpPublicBadge isPublic={!!toolset.mcpIsPublic} />
              </div>
            </div>
          </div>
          <ChevronRight className="h-5 w-5 text-muted-foreground group-hover:text-primary group-hover:translate-x-0.5 transition-all shrink-0 mt-2" />
        </div>

        {/* Description */}
        {toolset.description && (
          <Type className="text-sm text-muted-foreground line-clamp-2">
            {toolset.description}
          </Type>
        )}

        {/* Footer with tool count */}
        <div className="mt-4 pt-3 border-t">
          <Type className="text-xs text-muted-foreground">
            {toolset.toolUrns?.length || 0} tool
            {(toolset.toolUrns?.length || 0) !== 1 ? "s" : ""} available
          </Type>
        </div>
      </div>
    </routes.mcp.details.Link>
  );
}

function McpEnabledBadge({ enabled }: { enabled: boolean }) {
  if (enabled) {
    return (
      <Badge variant="success" className="gap-1">
        <Power size={12} />
        <Badge.Text>Enabled</Badge.Text>
      </Badge>
    );
  }

  return (
    <Badge variant="neutral" className="gap-1">
      <Power size={12} />
      <Badge.Text>Disabled</Badge.Text>
    </Badge>
  );
}

function McpPublicBadge({ isPublic }: { isPublic: boolean }) {
  if (isPublic) {
    return (
      <Badge variant="success" className="gap-1">
        <Globe size={12} />
        <Badge.Text>Public</Badge.Text>
      </Badge>
    );
  }

  return (
    <Badge variant="neutral" className="gap-1">
      <Lock size={12} />
      <Badge.Text>Private</Badge.Text>
    </Badge>
  );
}
