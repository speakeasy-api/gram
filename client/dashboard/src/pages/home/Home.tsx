import { MCPCard } from "@/components/mcp/MCPCard";
import { Page } from "@/components/page-layout";
import { Skeleton } from "@/components/ui/skeleton";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { useListToolsets } from "@gram/client/react-query";
import { Button, Stack } from "@speakeasy-api/moonshine";
import {
  ArrowRight,
  BlocksIcon,
  Code,
  MessageCircleIcon,
  ServerIcon,
} from "lucide-react";
import { useMemo } from "react";

export const LINKED_FROM_PARAM = "from";

export const onboardingStepStorageKeys = {
  test: "onboarding_playground_completed",
  curate: "onboarding_toolsets_completed",
  configure: "onboarding_mcp_config_completed",
};

export default function Home() {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const { data: toolsetsResult, isLoading: isToolsetsLoading } =
    useListToolsets();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  // Get the first public MCP toolset slug to pass to elements page
  const firstPublicToolsetSlug = useMemo(() => {
    if (!toolsetsResult?.toolsets) return undefined;
    const publicToolset = toolsetsResult.toolsets.find(
      (t) => t.mcpIsPublic && t.mcpEnabled,
    );
    return publicToolset?.slug;
  }, [toolsetsResult]);

  // MCP servers sorted by most recently updated
  const recentMcpServers = useMemo(() => {
    if (!toolsetsResult?.toolsets) return [];
    return [...toolsetsResult.toolsets]
      .filter((t) => t.mcpEnabled)
      .sort(
        (a, b) =>
          new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
      )
      .slice(0, 6);
  }, [toolsetsResult]);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        {/* MCP Servers Section */}
        {(isToolsetsLoading || recentMcpServers.length > 0) && (
          <div className="mb-10">
            <Stack
              direction="horizontal"
              justify="space-between"
              align="center"
              className="mb-4"
            >
              <h2 className="text-lg font-semibold">MCP Servers</h2>
              <routes.mcp.Link>
                <span className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm">
                  View all <ArrowRight className="h-4 w-4" />
                </span>
              </routes.mcp.Link>
            </Stack>
            <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
              {isToolsetsLoading &&
                [...Array(4)].map((_, i) => (
                  <Skeleton key={i} className="h-[120px] rounded-xl" />
                ))}
              {!isToolsetsLoading &&
                recentMcpServers.map((toolset) => (
                  <MCPCard key={toolset.id} toolset={toolset} />
                ))}
            </div>
          </div>
        )}

        <h2 className="mb-4 text-lg font-semibold">Quick actions</h2>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div className="bg-background relative flex flex-col gap-3 overflow-hidden rounded-lg border p-4 pb-5">
            <div className="bg-gradient-primary absolute inset-x-0 bottom-0 h-[3px]" />
            <div className="flex flex-row items-start gap-2">
              <MessageCircleIcon
                className="mt-0.5 h-[18px] w-[18px] shrink-0"
                strokeWidth={1.5}
              />
              <div className="flex flex-col gap-1">
                <h3 className="font-medium">Deploy chat</h3>
                <p className="text-muted-foreground text-sm">
                  Embed an AI chat interface on your website with tool access
                </p>
              </div>
            </div>
            <div className="mt-auto flex justify-end">
              <routes.elements.Link
                className="no-underline"
                queryParams={
                  firstPublicToolsetSlug
                    ? { toolset: firstPublicToolsetSlug }
                    : {}
                }
              >
                <Button size="sm">
                  <Button.Text>Get started</Button.Text>
                </Button>
              </routes.elements.Link>
            </div>
          </div>
          <div className="bg-background relative flex flex-col gap-3 overflow-hidden rounded-lg border p-4 pb-5">
            <div className="bg-gradient-primary absolute inset-x-0 bottom-0 h-[3px]" />
            <div className="flex flex-row items-start gap-2">
              <BlocksIcon
                className="mt-0.5 h-[18px] w-[18px] shrink-0"
                strokeWidth={1.5}
              />
              <div className="flex flex-col gap-1">
                <h3 className="font-medium">Connect to popular tools</h3>
                <p className="text-muted-foreground text-sm">
                  Browse and connect pre-built integrations from our catalog
                </p>
              </div>
            </div>
            <div className="mt-auto flex justify-end">
              <routes.catalog.Link className="no-underline">
                <Button size="sm">
                  <Button.Text>Browse catalog</Button.Text>
                </Button>
              </routes.catalog.Link>
            </div>
          </div>
          <div className="bg-background relative flex flex-col gap-3 overflow-hidden rounded-lg border p-4 pb-5">
            <div className="bg-gradient-primary absolute inset-x-0 bottom-0 h-[3px]" />
            <div className="flex flex-row items-start gap-2">
              <ServerIcon
                className="mt-0.5 h-[18px] w-[18px] shrink-0"
                strokeWidth={1.5}
              />
              <div className="flex flex-col gap-1">
                <h3 className="font-medium">Connect to existing APIs</h3>
                <p className="text-muted-foreground text-sm">
                  Create and deploy custom MCP servers from your APIs
                </p>
              </div>
            </div>
            <div className="mt-auto flex justify-end">
              <routes.sources.addOpenAPI.Link className="no-underline">
                <Button size="sm">
                  <Button.Text>Upload OpenAPI</Button.Text>
                </Button>
              </routes.sources.addOpenAPI.Link>
            </div>
          </div>
          {isFunctionsEnabled && (
            <div className="bg-background relative flex flex-col gap-3 overflow-hidden rounded-lg border p-4 pb-5">
              <div className="bg-gradient-primary absolute inset-x-0 bottom-0 h-[3px]" />
              <div className="flex flex-row items-start gap-2">
                <Code
                  className="mt-0.5 h-[18px] w-[18px] shrink-0"
                  strokeWidth={1.5}
                />
                <div className="flex flex-col gap-1">
                  <h3 className="font-medium">Build and host custom tools</h3>
                  <p className="text-muted-foreground text-sm">
                    Write and deploy custom functions as MCP servers
                  </p>
                </div>
              </div>
              <div className="mt-auto flex justify-end">
                <routes.sources.addFunction.Link className="no-underline">
                  <Button size="sm">
                    <Button.Text>Deploy code</Button.Text>
                  </Button>
                </routes.sources.addFunction.Link>
              </div>
            </div>
          )}
        </div>
      </Page.Body>
    </Page>
  );
}
