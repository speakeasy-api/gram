import { MCPCard } from "@/components/mcp/MCPCard";
import { Page } from "@/components/page-layout";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { Server, useInfiniteListMCPCatalog } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { DeploymentExternalMCP } from "@gram/client/models/components";
import { useLatestDeployment } from "@/hooks/toolTypes";
import { useListToolsets } from "@gram/client/react-query";
import { Badge, Button, Stack } from "@speakeasy-api/moonshine";
import {
  ArrowRight,
  BlocksIcon,
  CheckCircle,
  Code,
  MessageCircleIcon,
  ServerIcon,
} from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";

export const LINKED_FROM_PARAM = "from";

export const onboardingStepStorageKeys = {
  test: "onboarding_playground_completed",
  curate: "onboarding_toolsets_completed",
  configure: "onboarding_mcp_config_completed",
};

// Featured server specifiers - well-known brands from the catalog
const FEATURED_SERVER_SPECIFIERS = [
  "com.figma.mcp/mcp",
  "com.stripe/mcp",
  "app.linear/linear",
  "io.github.getsentry/sentry-mcp",
  "io.github.github/github-mcp-server",
  "com.notion/mcp",
];

export default function Home() {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const { data, isLoading } = useInfiniteListMCPCatalog();
  const { data: deploymentResult } = useLatestDeployment();
  const { data: toolsetsResult, isLoading: isToolsetsLoading } =
    useListToolsets();
  const externalMcps = deploymentResult?.deployment?.externalMcps ?? [];
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  const featuredServers = useMemo(() => {
    if (!data?.pages) return [];
    const allServers = data.pages.flatMap((page) => page.servers as Server[]);
    return FEATURED_SERVER_SPECIFIERS.map((specifier) =>
      allServers.find((s) => s.registrySpecifier === specifier),
    ).filter((s): s is Server => s !== undefined);
  }, [data]);

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
                <span className="text-sm text-muted-foreground hover:text-foreground flex items-center gap-1">
                  View all <ArrowRight className="w-4 h-4" />
                </span>
              </routes.mcp.Link>
            </Stack>
            <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
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

        <h2 className="text-lg font-semibold mb-4">Quick actions</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="relative flex flex-col gap-3 rounded-lg border bg-background p-4 pb-5 overflow-hidden">
            <div className="absolute bottom-0 inset-x-0 h-[3px] bg-gradient-primary" />
            <div className="flex flex-row items-start gap-2">
              <MessageCircleIcon
                className="h-[18px] w-[18px] mt-0.5 shrink-0"
                strokeWidth={1.5}
              />
              <div className="flex flex-col gap-1">
                <h3 className="font-medium">Deploy chat</h3>
                <p className="text-sm text-muted-foreground">
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
          <div className="relative flex flex-col gap-3 rounded-lg border bg-background p-4 pb-5 overflow-hidden">
            <div className="absolute bottom-0 inset-x-0 h-[3px] bg-gradient-primary" />
            <div className="flex flex-row items-start gap-2">
              <BlocksIcon
                className="h-[18px] w-[18px] mt-0.5 shrink-0"
                strokeWidth={1.5}
              />
              <div className="flex flex-col gap-1">
                <h3 className="font-medium">Connect to popular tools</h3>
                <p className="text-sm text-muted-foreground">
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
          <div className="relative flex flex-col gap-3 rounded-lg border bg-background p-4 pb-5 overflow-hidden">
            <div className="absolute bottom-0 inset-x-0 h-[3px] bg-gradient-primary" />
            <div className="flex flex-row items-start gap-2">
              <ServerIcon
                className="h-[18px] w-[18px] mt-0.5 shrink-0"
                strokeWidth={1.5}
              />
              <div className="flex flex-col gap-1">
                <h3 className="font-medium">Connect to existing APIs</h3>
                <p className="text-sm text-muted-foreground">
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
            <div className="relative flex flex-col gap-3 rounded-lg border bg-background p-4 pb-5 overflow-hidden">
              <div className="absolute bottom-0 inset-x-0 h-[3px] bg-gradient-primary" />
              <div className="flex flex-row items-start gap-2">
                <Code
                  className="h-[18px] w-[18px] mt-0.5 shrink-0"
                  strokeWidth={1.5}
                />
                <div className="flex flex-col gap-1">
                  <h3 className="font-medium">Build and host custom tools</h3>
                  <p className="text-sm text-muted-foreground">
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

        {/* Featured Servers Section */}
        {(isLoading || featuredServers.length > 0) && (
          <div className="mt-10">
            <Stack
              direction="horizontal"
              justify="space-between"
              align="center"
              className="mb-4"
            >
              <h2 className="text-lg font-semibold">
                Featured Third-Party Servers
              </h2>
              <routes.catalog.Link>
                <span className="text-sm text-muted-foreground hover:text-foreground flex items-center gap-1">
                  Browse all <ArrowRight className="w-4 h-4" />
                </span>
              </routes.catalog.Link>
            </Stack>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {isLoading &&
                [...Array(6)].map((_, i) => (
                  <Skeleton key={i} className="h-[140px] rounded-xl" />
                ))}
              {!isLoading &&
                featuredServers.map((server) => (
                  <FeaturedServerCard
                    key={server.registrySpecifier}
                    server={server}
                    detailHref={routes.catalog.detail.href(
                      encodeURIComponent(server.registrySpecifier),
                    )}
                    externalMcps={externalMcps}
                  />
                ))}
            </div>
          </div>
        )}
      </Page.Body>
    </Page>
  );
}

function FeaturedServerCard({
  server,
  detailHref,
  externalMcps,
}: {
  server: Server;
  detailHref: string;
  externalMcps: DeploymentExternalMCP[];
}) {
  const meta = server.meta["com.pulsemcp/server"];
  const isOfficial = meta?.isOfficial;
  const visitorsTotal = meta?.visitorsEstimateLastFourWeeks;
  const displayName = server.title ?? server.registrySpecifier;

  const isAdded = externalMcps.some(
    (mcp) => mcp.registryServerSpecifier === server.registrySpecifier,
  );

  return (
    <Link to={detailHref}>
      <div className="group flex flex-col gap-4 rounded-xl border bg-card p-5 hover:border-primary/50 hover:shadow-md transition-all h-full">
        <Stack direction="horizontal" gap={3}>
          <div className="w-12 h-12 rounded-lg bg-primary/10 flex items-center justify-center shrink-0 group-hover:bg-primary/15 transition-colors">
            {server.iconUrl ? (
              <img
                src={server.iconUrl}
                alt={displayName}
                className="w-8 h-8 rounded"
              />
            ) : (
              <ServerIcon className="w-6 h-6 text-muted-foreground" />
            )}
          </div>
          <Stack gap={1} className="min-w-0">
            <Stack direction="horizontal" gap={2} align="center">
              <Type
                variant="subheading"
                className="group-hover:text-primary transition-colors"
              >
                {displayName}
              </Type>
              {isOfficial && <Badge>Official</Badge>}
            </Stack>
            <Type small muted>
              {server.registrySpecifier} • v{server.version}
            </Type>
          </Stack>
        </Stack>
        <Type small muted className="line-clamp-2">
          {server.description}
        </Type>
        <div className="mt-auto pt-2">
          <Stack direction="horizontal" justify="space-between" align="center">
            {visitorsTotal && visitorsTotal > 0 ? (
              <Type small muted>
                {visitorsTotal.toLocaleString()} monthly users
              </Type>
            ) : (
              <div />
            )}
            {isAdded ? (
              <Button variant="secondary" size="sm">
                <Button.LeftIcon>
                  <CheckCircle className="w-3.5 h-3.5" />
                </Button.LeftIcon>
                <Button.Text>Added</Button.Text>
              </Button>
            ) : (
              <Button variant="secondary" size="sm">
                <Button.Text>Add</Button.Text>
              </Button>
            )}
          </Stack>
        </div>
      </div>
    </Link>
  );
}
