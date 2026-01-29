import { Page } from "@/components/page-layout";
import { cn } from "@/lib/utils";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useUser } from "@/contexts/Auth";
import { Server, useInfiniteListMCPCatalog } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { DeploymentExternalMCP } from "@gram/client/models/components";
import { useLatestDeployment, useListToolsets } from "@gram/client/react-query";
import { Badge, Button, Stack } from "@speakeasy-api/moonshine";
import {
  ArrowRight,
  BlocksIcon,
  CheckCircle,
  Database,
  Globe,
  MessageCircleIcon,
  ServerIcon,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";

function useWindowWidth() {
  const [width, setWidth] = useState(
    typeof window !== "undefined" ? window.innerWidth : 1200,
  );

  useEffect(() => {
    const handleResize = () => setWidth(window.innerWidth);
    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  }, []);

  return width;
}

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
  "io.github.aws/mcp-proxy-for-aws",
  "io.github.grafana/mcp-grafana",
];

export default function Home() {
  const routes = useRoutes();
  const user = useUser();
  const { data, isLoading } = useInfiniteListMCPCatalog();
  const { data: deploymentResult, isLoading: isDeploymentLoading } =
    useLatestDeployment();
  const { data: toolsetsResult, isLoading: isToolsetsLoading } =
    useListToolsets();
  const externalMcps = deploymentResult?.deployment?.externalMcps ?? [];
  const deployment = deploymentResult?.deployment;

  const featuredServers = useMemo(() => {
    if (!data?.pages) return [];
    const allServers = data.pages.flatMap((page) => page.servers as Server[]);
    return FEATURED_SERVER_SPECIFIERS.map((specifier) =>
      allServers.find((s) => s.registrySpecifier === specifier),
    ).filter((s): s is Server => s !== undefined);
  }, [data]);

  const firstName = user.displayName?.split(" ")[0];

  // Setup completion state
  const hasSource = useMemo(() => {
    if (!deployment) return false;
    return (
      deployment.openapiv3Assets.length > 0 ||
      (deployment.functionsAssets?.length ?? 0) > 0 ||
      (deployment.externalMcps?.length ?? 0) > 0
    );
  }, [deployment]);

  const hasEnabledMcpWithTools = useMemo(() => {
    if (!toolsetsResult?.toolsets) return false;
    return toolsetsResult.toolsets.some(
      (t) => t.mcpEnabled && t.toolUrns.length > 0,
    );
  }, [toolsetsResult]);

  // Get the first public MCP toolset slug to pass to elements page
  const firstPublicToolsetSlug = useMemo(() => {
    if (!toolsetsResult?.toolsets) return undefined;
    const publicToolset = toolsetsResult.toolsets.find(
      (t) => t.mcpIsPublic && t.mcpEnabled,
    );
    return publicToolset?.slug;
  }, [toolsetsResult]);

  // Check localStorage for chat/claude setup completion
  // Only count as complete if prior steps are also complete
  const hasDeployedChatFlag =
    typeof window !== "undefined" &&
    localStorage.getItem(onboardingStepStorageKeys.configure) === "true";
  const hasDeployedChat =
    hasSource && hasEnabledMcpWithTools && hasDeployedChatFlag;

  const completedSteps = [
    hasSource,
    hasEnabledMcpWithTools,
    hasDeployedChat,
  ].filter(Boolean).length;
  const isSetupComplete = completedSteps === 3;
  const isSetupDataLoading = isDeploymentLoading || isToolsetsLoading;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <h1 className="text-2xl font-semibold mb-6">
          Welcome{firstName ? `, ${firstName}` : ""}
        </h1>

        {/* Setup Progress Widget */}
        <SetupSteps
          isSetupDataLoading={isSetupDataLoading}
          isSetupComplete={isSetupComplete}
          hasSource={hasSource}
          hasEnabledMcpWithTools={hasEnabledMcpWithTools}
          hasDeployedChat={hasDeployedChat}
          routes={routes}
          firstPublicToolsetSlug={firstPublicToolsetSlug}
        />

        <h2 className="text-lg font-semibold mb-4">Quick actions</h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
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
                <h3 className="font-medium">Host your own tools</h3>
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
        </div>

        {/* Featured Servers Section */}
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

function SetupSteps({
  isSetupDataLoading,
  isSetupComplete,
  hasSource,
  hasEnabledMcpWithTools,
  hasDeployedChat,
  routes,
  firstPublicToolsetSlug,
}: {
  isSetupDataLoading: boolean;
  isSetupComplete: boolean;
  hasSource: boolean;
  hasEnabledMcpWithTools: boolean;
  hasDeployedChat: boolean;
  routes: ReturnType<typeof useRoutes>;
  firstPublicToolsetSlug: string | undefined;
}) {
  const windowWidth = useWindowWidth();
  const isVertical = windowWidth < 1000;

  if (isSetupDataLoading || isSetupComplete) {
    return null;
  }

  return (
    <div className="mb-8">
      <div
        className={`flex items-stretch ${isVertical ? "flex-col" : "flex-row"}`}
      >
        <SetupStep
          number={1}
          title="Add a source"
          description="Connect an API, function, or third-party server"
          completed={hasSource}
          enabled={true}
          href={routes.sources.href()}
          icon={<Database className="h-[18px] w-[18px]" strokeWidth={1.5} />}
          position="first"
          cta="Add source"
          isVertical={isVertical}
        />
        <SetupStep
          number={2}
          title="Add an MCP server"
          description="Enable an MCP server connected to your source"
          completed={hasEnabledMcpWithTools}
          enabled={hasSource}
          href={routes.mcp.href()}
          icon={<Globe className="h-[18px] w-[18px]" strokeWidth={1.5} />}
          position="middle"
          cta="Configure"
          isVertical={isVertical}
        />
        <SetupStep
          number={3}
          title="Deploy chat or connect"
          description="Embed chat on your site or connect via Claude"
          completed={hasDeployedChat}
          enabled={hasSource && hasEnabledMcpWithTools}
          href={`${routes.elements.href()}${firstPublicToolsetSlug ? `?toolset=${firstPublicToolsetSlug}` : ""}`}
          icon={
            <MessageCircleIcon
              className="h-[18px] w-[18px]"
              strokeWidth={1.5}
            />
          }
          position="last"
          cta="Deploy"
          isVertical={isVertical}
        />
      </div>
    </div>
  );
}

function SetupStep({
  number,
  title,
  description,
  completed,
  enabled,
  href,
  icon: _icon,
  position,
  cta,
  isVertical,
}: {
  number: number;
  title: string;
  description: string;
  completed: boolean;
  enabled: boolean;
  href: string;
  icon: React.ReactNode;
  position: "first" | "middle" | "last";
  cta: string;
  isVertical: boolean;
}) {
  const isActive = enabled && !completed;
  const bgColor = completed
    ? "bg-emerald-500/10"
    : isActive
      ? "bg-primary/10"
      : enabled
        ? "bg-background"
        : "bg-muted/30";

  // Horizontal arrow clip-paths (pointing right)
  const horizontalClipPaths = {
    first:
      "polygon(0 0, calc(100% - 20px) 0, 100% 50%, calc(100% - 20px) 100%, 0 100%)",
    middle:
      "polygon(0 0, calc(100% - 20px) 0, 100% 50%, calc(100% - 20px) 100%, 0 100%, 20px 50%)",
    last: "polygon(0 0, 100% 0, 100% 100%, 0 100%, 20px 50%)",
  };

  // Vertical arrow clip-paths (pointing down) - fixed arrow size (80px base, 20px depth)
  const verticalClipPaths = {
    first:
      "polygon(0 0, 100% 0, 100% calc(100% - 20px), calc(50% + 40px) calc(100% - 20px), 50% 100%, calc(50% - 40px) calc(100% - 20px), 0 calc(100% - 20px))",
    middle:
      "polygon(0 0, calc(50% - 40px) 0, 50% 20px, calc(50% + 40px) 0, 100% 0, 100% calc(100% - 20px), calc(50% + 40px) calc(100% - 20px), 50% 100%, calc(50% - 40px) calc(100% - 20px), 0 calc(100% - 20px))",
    last: "polygon(0 0, calc(50% - 40px) 0, 50% 20px, calc(50% + 40px) 0, 100% 0, 100% 100%, 0 100%)",
  };

  const clipPath = isVertical
    ? verticalClipPaths[position]
    : horizontalClipPaths[position];

  const content = (
    <div
      className={cn(
        "group relative flex flex-row items-start gap-3 py-5 pr-8 transition-all h-full flex-1",
        bgColor,
        isActive && "hover:bg-primary/15",
        !enabled && "opacity-60",
        isVertical && "pl-6",
        isVertical && position !== "first" && "-mt-[10px]",
        isVertical && (position === "first" ? "pt-7" : "pt-12"),
        isVertical && (position === "last" ? "pb-7" : "pb-12"),
        !isVertical && position !== "first" && "-ml-[10px]",
        !isVertical && (position === "first" ? "pl-4" : "pl-9"),
      )}
      style={{ clipPath }}
    >
      <div
        className={`flex items-center justify-center w-7 h-7 rounded-full shrink-0 border ${
          isActive ? "bg-white dark:bg-white border-primary" : "bg-background"
        }`}
      >
        {completed ? (
          <CheckCircle className="h-4 w-4 text-emerald-500" strokeWidth={2} />
        ) : (
          <span
            className={`text-sm font-medium ${isActive ? "text-primary" : "text-muted-foreground"}`}
          >
            {number}
          </span>
        )}
      </div>
      <div className="flex flex-col gap-1.5 min-w-0 flex-1">
        <h3 className="font-medium text-sm">{title}</h3>
        <Type small muted>
          {description}
        </Type>
        {completed ? (
          <Button size="sm" disabled className="mt-1 w-fit">
            <Button.LeftIcon>
              <CheckCircle className="w-3 h-3" />
            </Button.LeftIcon>
            <Button.Text>Done</Button.Text>
          </Button>
        ) : (
          <Button size="sm" disabled={!enabled} className="mt-1 w-fit">
            <Button.Text>{cta}</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="w-3 h-3" />
            </Button.RightIcon>
          </Button>
        )}
      </div>
    </div>
  );

  if (!enabled || completed) {
    return <div className="flex-1 flex">{content}</div>;
  }

  return (
    <Link to={href} className="no-underline flex-1 flex">
      {content}
    </Link>
  );
}
