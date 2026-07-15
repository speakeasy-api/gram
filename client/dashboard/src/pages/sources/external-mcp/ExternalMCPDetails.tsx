import { Page } from "@/components/page-layout";
import { McpServerCardsSkeleton } from "@/components/sources/McpServerCardsSkeleton";
import { MCPServerPortalCard } from "@/components/sources/MCPServerPortalCard";
import { RemoveSourceDialogContent } from "@/components/sources/RemoveSourceDialogContent";
import { ExternalMCPIllustration } from "@/components/sources/SourceCardIllustrations";
import { useCatalogIconMap } from "@/components/sources/sources-hooks";
import { Heading } from "@/components/ui/heading";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { attachmentToURNPrefix } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { RequireScope } from "@/components/require-scope";
import { Dialog } from "@/components/ui/dialog";
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";

import { Server, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Navigate, useNavigate, useParams } from "react-router";
import { toast } from "sonner";

export default function ExternalMCPDetails(): JSX.Element {
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
  const sourceOrigin = useMemo(() => {
    if (source?.organizationMcpCollectionRegistryId) {
      return {
        id: source.organizationMcpCollectionRegistryId,
        label: "Collection",
      };
    }

    if (source?.registryId) {
      return {
        id: source.registryId,
        label: "Catalog",
      };
    }

    return {
      id: undefined,
      label: "External MCP",
    };
  }, [source]);

  const { data: toolsets, isLoading: isLoadingToolsets } = useListToolsets();

  // Find ALL toolsets that use this external MCP source (could be multiple).
  // A catalog-imported source contributes one URN per registry tool
  // (`tools:externalmcp:<slug>:<toolName>`); only the no-tools fallback uses
  // `:proxy`. Match the source-scoped prefix so both shapes are detected.
  const associatedToolsets = useMemo(() => {
    if (!toolsets?.toolsets || !source) return [];

    const urnPrefix = attachmentToURNPrefix("externalmcp", source.slug);
    return toolsets.toolsets.filter((t) =>
      t.toolUrns?.some((urn) => urn.startsWith(urnPrefix)),
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
      void navigate(routes.sources.href());
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
                <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
                <PageTabsTrigger value="mcp-servers">
                  MCP Servers{" "}
                  {associatedToolsets.length > 0 &&
                    `(${associatedToolsets.length})`}
                </PageTabsTrigger>
                <PageTabsTrigger value="settings">Settings</PageTabsTrigger>
              </TabsList>
            </div>
          </div>

          {/* Overview Tab */}
          <TabsContent
            value="overview"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            <div className="mx-auto w-full max-w-[1270px] space-y-6 px-8 py-8">
              {/* Row 1: Name, Origin, Origin ID */}
              <div className="flex flex-wrap gap-x-16 gap-y-6">
                <div>
                  <Type muted small className="mb-1">
                    Name
                  </Type>
                  <Type className="font-medium">{source?.name || "—"}</Type>
                </div>
                <div>
                  <Type muted small className="mb-1">
                    Created from
                  </Type>
                  <Badge variant="neutral">
                    <Badge.Text>{sourceOrigin.label}</Badge.Text>
                  </Badge>
                </div>
                <div>
                  <Type muted small className="mb-1">
                    Origin ID
                  </Type>
                  <Type className="font-mono break-all">
                    {sourceOrigin.id || "—"}
                  </Type>
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
          <TabsContent
            value="mcp-servers"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
              {isLoadingToolsets ? (
                <McpServerCardsSkeleton />
              ) : associatedToolsets.length > 0 ? (
                <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
                  {associatedToolsets.map((toolset) => (
                    <MCPServerPortalCard key={toolset.slug} toolset={toolset} />
                  ))}
                </div>
              ) : (
                <InlineEmptyState
                  icon={<Server />}
                  title="No MCP servers are using this source yet"
                />
              )}
            </div>
          </TabsContent>

          {/* Settings Tab */}
          <TabsContent
            value="settings"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
              {/* Danger Zone */}
              <div className="border-destructive/30 border p-6">
                <Type variant="subheading" className="text-destructive mb-1">
                  Danger Zone
                </Type>
                <Type muted small className="mb-4">
                  Removing this source will remove it from the current
                  deployment. This action cannot be undone.
                </Type>
                <RequireScope scope="project:write" level="component">
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
                </RequireScope>
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
