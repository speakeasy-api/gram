import { Page } from "@/components/page-layout";
import { RemoveSourceDialogContent } from "@/components/sources/RemoveSourceDialogContent";
import { ExternalMCPIllustration } from "@/components/sources/SourceCardIllustrations";
import { useCatalogIconMap } from "@/components/sources/sources-hooks";
import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import {
  useLatestDeployment,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { ToolsetEntry } from "@gram/client/models/components";
import { Badge, Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { ChevronRight, Globe, Lock, Power, Server, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Navigate, useNavigate, useParams } from "react-router";
import { toast } from "sonner";

export default function ExternalMCPDetails() {
  const { sourceSlug } = useParams<{
    sourceSlug: string;
  }>();
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();
  const catalogIconMap = useCatalogIconMap();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  // Valid tabs
  const validTabs = ["overview", "mcp-servers", "settings"];

  // Tab state from URL hash
  const [activeTab, setActiveTab] = useState(() => {
    const hash = window.location.hash.replace("#", "");
    return validTabs.includes(hash) ? hash : "overview";
  });

  const handleTabChange = (value: string) => {
    setActiveTab(value);
    const url = new URL(window.location.href);
    url.hash = value;
    window.history.replaceState(null, "", url.toString());
  };

  const {
    data: deployment,
    isLoading: isLoadingDeployment,
    refetch,
  } = useLatestDeployment();

  // Find the specific external MCP server from the deployment
  const source = useMemo(() => {
    if (!deployment?.deployment) return null;

    return deployment.deployment.externalMcps?.find(
      (mcp) => mcp.slug === sourceSlug,
    );
  }, [deployment, sourceSlug]);

  const { data: toolsets, isLoading: isLoadingToolsets } = useListToolsets();

  // Find ALL toolsets that use this external MCP source (could be multiple)
  const associatedToolsets = useMemo(() => {
    if (!toolsets?.toolsets || !source) return [];

    return toolsets.toolsets.filter((t) =>
      t.toolUrns?.includes(`tools:externalmcp:${source.slug}:proxy`),
    );
  }, [toolsets, source]);

  const handleRemoveSource = async (
    slug: string,
    _type: "openapi" | "function" | "externalmcp",
  ) => {
    try {
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.deployment?.id,
          nonBlocking: true,
          excludeExternalMcps: [slug],
        },
      });
      await refetch();
      toast.success("External MCP source deleted successfully");
      navigate(routes.sources.href());
    } catch (error) {
      console.error("Failed to delete external MCP source:", error);
      toast.error("Failed to delete external MCP source. Please try again.");
    }
  };

  // If source not found, redirect to sources index
  if (!isLoadingDeployment && !source) {
    return <Navigate to={routes.sources.href()} replace />;
  }

  // Create asset object for delete dialog
  const assetForDialog = source
    ? {
        id: source.slug,
        deploymentAssetId: source.slug,
        name: source.name,
        slug: source.slug,
        type: "externalmcp" as const,
        registryId: source.registryId,
        iconUrl: catalogIconMap.get(source.registryServerSpecifier || ""),
      }
    : null;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [sourceSlug || ""]: source?.name }}
          skipSegments={["externalmcp"]}
        />
      </Page.Header>

      <Page.Body fullWidth noPadding fullHeight overflowHidden>
        {/* Hero Header with Illustration - full width */}
        <div className="relative h-64 w-full shrink-0 overflow-hidden">
          <ExternalMCPIllustration
            logoUrl={catalogIconMap.get(source?.registryServerSpecifier || "")}
            name={source?.name}
            slug={sourceSlug || ""}
            className="scale-200"
          />

          {/* Overlay for text readability */}
          <div className="from-foreground/50 via-foreground/20 absolute inset-0 bg-linear-to-t to-transparent" />
          <div className="absolute right-0 bottom-0 left-0 mx-auto w-full max-w-[1270px] px-8 py-8">
            <Stack gap={2}>
              <div className="ml-1 flex items-center gap-3">
                <Heading variant="h1" className="text-background">
                  {source?.name || sourceSlug}
                </Heading>
                <Badge variant="neutral">
                  <Badge.Text>External MCP</Badge.Text>
                </Badge>
              </div>
              <div className="ml-1 flex items-center gap-2">
                <Type className="text-background/70! max-w-2xl truncate">
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
          className="flex min-h-0 w-full flex-1 flex-col"
        >
          <div className="shrink-0 border-b">
            <div className="mx-auto max-w-[1270px] px-8">
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <TabsTrigger
                  value="overview"
                  className="text-muted-foreground data-[state=active]:text-foreground data-[state=active]:after:bg-primary relative h-11 rounded-none border-none bg-transparent! px-1 pt-3 pb-3 shadow-none! after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5 after:bg-transparent data-[state=active]:bg-transparent!"
                >
                  Overview
                </TabsTrigger>
                <TabsTrigger
                  value="mcp-servers"
                  className="text-muted-foreground data-[state=active]:text-foreground data-[state=active]:after:bg-primary relative h-11 rounded-none border-none bg-transparent! px-1 pt-3 pb-3 shadow-none! after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5 after:bg-transparent data-[state=active]:bg-transparent!"
                >
                  MCP Servers{" "}
                  {associatedToolsets.length > 0 &&
                    `(${associatedToolsets.length})`}
                </TabsTrigger>
                <TabsTrigger
                  value="settings"
                  className="text-muted-foreground data-[state=active]:text-foreground data-[state=active]:after:bg-primary relative h-11 rounded-none border-none bg-transparent! px-1 pt-3 pb-3 shadow-none! after:absolute after:right-0 after:bottom-0 after:left-0 after:h-0.5 after:bg-transparent data-[state=active]:bg-transparent!"
                >
                  Settings
                </TabsTrigger>
              </TabsList>
            </div>
          </div>

          {/* Overview Tab */}
          <TabsContent value="overview" className="mt-0 flex-1">
            <div className="mx-auto w-full max-w-[1270px] space-y-6 px-8 py-8">
              {/* Row 1: Name, Registry ID */}
              <div className="flex gap-16">
                <div>
                  <Type muted small className="mb-1">
                    Name
                  </Type>
                  <Type className="font-medium">{source?.name || "—"}</Type>
                </div>
                <div>
                  <Type muted small className="mb-1">
                    Registry ID
                  </Type>
                  <Type className="font-mono">{source?.registryId || "—"}</Type>
                </div>
              </div>

              {/* Row 2: Server Specifier */}
              <div className="flex gap-16">
                <div>
                  <Type muted small className="mb-1">
                    Server Specifier
                  </Type>
                  <Type className="font-mono break-all">
                    {source?.registryServerSpecifier || "—"}
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
                    className="text-primary font-mono hover:underline"
                  >
                    {deployment.deployment.id.slice(0, 8)}
                  </routes.deployments.deployment.Link>
                ) : (
                  <Type className="text-muted-foreground">None</Type>
                )}
              </div>
            </div>
          </TabsContent>

          {/* MCP Servers Tab */}
          <TabsContent value="mcp-servers" className="mt-0 flex-1">
            <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
              {isLoadingToolsets ? (
                <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                  {[1, 2, 3].map((i) => (
                    <div
                      key={i}
                      className="bg-card animate-pulse rounded-xl border p-6"
                    >
                      <div className="mb-4 flex items-center gap-3">
                        <div className="bg-muted h-10 w-10 rounded-lg" />
                        <div className="flex-1">
                          <div className="bg-muted mb-2 h-4 w-24 rounded" />
                          <div className="bg-muted h-3 w-32 rounded" />
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              ) : associatedToolsets.length > 0 ? (
                <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                  {associatedToolsets.map((toolset) => (
                    <MCPServerPortalCard key={toolset.slug} toolset={toolset} />
                  ))}
                </div>
              ) : (
                <div className="py-12 text-center">
                  <Server className="text-muted-foreground/50 mx-auto mb-3 h-12 w-12" />
                  <Type muted>No MCP servers are using this source yet.</Type>
                </div>
              )}
            </div>
          </TabsContent>

          {/* Settings Tab */}
          <TabsContent value="settings" className="mt-0 flex-1">
            <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
              {/* Danger Zone */}
              <div className="border-destructive/30 rounded-lg border p-6">
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
      className="group bg-card hover:bg-surface-secondary hover:border-primary/30 block cursor-pointer rounded-xl border transition-all duration-200 hover:no-underline hover:shadow-lg"
    >
      <div className="p-5">
        {/* Header with icon */}
        <div className="mb-3 flex items-start justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="bg-primary/10 flex h-10 w-10 items-center justify-center rounded-lg">
              <Server className="text-primary h-5 w-5" />
            </div>
            <div>
              <Type className="group-hover:text-primary text-base font-semibold transition-colors">
                {toolset.name}
              </Type>
              <div className="mt-1 flex items-center gap-2">
                <McpEnabledBadge enabled={!!toolset.mcpEnabled} />
                <McpPublicBadge isPublic={!!toolset.mcpIsPublic} />
              </div>
            </div>
          </div>
          <ChevronRight className="text-muted-foreground group-hover:text-primary mt-2 h-5 w-5 shrink-0 transition-all group-hover:translate-x-0.5" />
        </div>

        {/* Description */}
        {toolset.description && (
          <Type className="text-muted-foreground line-clamp-2 text-sm">
            {toolset.description}
          </Type>
        )}

        {/* Footer with tool count */}
        <div className="mt-4 border-t pt-3">
          <Type className="text-muted-foreground text-xs">
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
