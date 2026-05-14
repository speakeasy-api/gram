import { Block, BlockInner } from "@/components/block";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { CodeBlock } from "@/components/code";
import { DetailHero } from "@/components/detail-hero";
import { DotCard } from "@/components/ui/dot-card";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useCustomDomains, useMcpEndpointUrl } from "@/hooks/useToolsetUrl";
import {
  formatRemoteMcpUrlForDisplay,
  getMcpServerArgs,
  mcpServerRouteParam,
  remoteMcpRouteParam,
} from "@/lib/sources";
import { cn } from "@/lib/utils";
import { useOrgRoutes, useRoutes } from "@/routes";
import { useRBAC } from "@/hooks/useRBAC";
import type {
  CustomDomain,
  McpEndpoint,
  McpServer,
  McpServerVisibility,
  ToolsetEntry,
} from "@gram/client/models/components";
import {
  invalidateAllGetMcpServer,
  invalidateAllMcpEndpoints,
  invalidateAllMcpServers,
  useDeleteMcpEndpointMutation,
  useDeleteMcpServerMutation,
  useGetMcpServer,
  useGetRemoteMcpServer,
  useListToolsets,
  useMcpEndpoints,
  useUpdateMcpEndpointMutation,
  useUpdateMcpServerMutation,
} from "@gram/client/react-query/index.js";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Alert,
  Badge,
  Button,
  Dialog,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Stack,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  ChevronDown,
  Loader2,
  Network,
  Plus,
  Trash2,
  Wrench,
} from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Navigate, useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { MCPStatusIndicator } from "@/components/mcp/MCPStatusIndicator";
import { MCPTeamAccessTab } from "../MCPTeamAccessTab";
import { useMcpEndpointSlugValidation } from "./useMcpEndpointSlugValidation";

const VALID_TABS = [
  "overview",
  "endpoints",
  "team-access",
  "settings",
] as const;
type TabValue = (typeof VALID_TABS)[number];

function isValidTab(value: string): value is TabValue {
  return (VALID_TABS as readonly string[]).includes(value);
}

export default function MCPServerDetails() {
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isRemoteMcpEnabled =
    telemetry.isFeatureEnabled("gram-remote-mcp") ?? false;
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;
  const idOrSlug = mcpServerSlug ?? "";

  const [activeTab, setActiveTab] = useState<TabValue>(() => {
    const hash = window.location.hash.replace("#", "");
    if (!isValidTab(hash)) return "overview";
    if (hash === "team-access" && !isRbacEnabled) return "overview";
    return hash;
  });

  const handleTabChange = (value: string) => {
    if (!isValidTab(value)) return;
    setActiveTab(value);
    const url = new URL(window.location.href);
    url.hash = value;
    window.history.replaceState(null, "", url.toString());
  };

  const {
    data: mcpServer,
    isLoading,
    isError,
  } = useGetMcpServer(getMcpServerArgs(idOrSlug), undefined, {
    enabled: isRemoteMcpEnabled && idOrSlug !== "",
  });

  const mcpServerId = mcpServer?.id ?? "";

  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ mcpServerId }, undefined, {
      enabled: isRemoteMcpEnabled && mcpServerId !== "",
    });
  const endpoints = endpointsResult?.mcpEndpoints ?? [];

  if (!isRemoteMcpEnabled) {
    return <Navigate to={routes.mcp.href()} replace />;
  }
  if (isError || (!isLoading && !mcpServer)) {
    return <Navigate to={routes.mcp.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [idOrSlug]: mcpServer?.name || "MCP Server",
          }}
          skipSegments={["x"]}
        />
      </Page.Header>

      <Page.Body
        fullWidth
        noPadding
        fullHeight
        overflowHidden
        className="gap-0"
      >
        <MCPServerHero server={mcpServer} />

        <Tabs
          value={activeTab}
          onValueChange={handleTabChange}
          className="flex min-h-0 w-full flex-1 flex-col"
        >
          <div className="shrink-0 border-b">
            <div className="mx-auto max-w-[1270px] px-8">
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
                <PageTabsTrigger value="endpoints">
                  Endpoints
                  {endpoints.length > 0 && ` (${endpoints.length})`}
                </PageTabsTrigger>
                {isRbacEnabled && (
                  <PageTabsTrigger value="team-access">
                    Team Access
                  </PageTabsTrigger>
                )}
                <PageTabsTrigger value="settings">Settings</PageTabsTrigger>
              </TabsList>
            </div>
          </div>

          <TabsContent
            value="overview"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            <OverviewTab
              mcpServer={mcpServer}
              endpoints={endpoints}
              isLoadingEndpoints={isLoadingEndpoints}
              onShowEndpoints={() => handleTabChange("endpoints")}
            />
          </TabsContent>

          <TabsContent
            value="endpoints"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            {mcpServer && (
              <EndpointsTab
                mcpServer={mcpServer}
                endpoints={endpoints}
                isLoadingEndpoints={isLoadingEndpoints}
              />
            )}
          </TabsContent>

          {isRbacEnabled && mcpServer && (
            <TabsContent
              value="team-access"
              className="mt-0 min-h-0 flex-1 overflow-y-auto"
            >
              <RequireScope scope="mcp:read" level="page">
                <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
                  {/* mcp_servers-backed servers grant under the same `mcp:*`
                      scope kind as toolset-backed ones (see selector.go), so
                      MCPTeamAccessTab is reused as-is with the mcp_server's
                      id as the resource id. No `tools` prop because the
                      Remote MCP backend doesn't expose a Gram-side tool
                      catalog. */}
                  <MCPTeamAccessTab resourceId={mcpServer.id} />
                </div>
              </RequireScope>
            </TabsContent>
          )}

          <TabsContent
            value="settings"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            {mcpServer && (
              <SettingsTab mcpServer={mcpServer} endpoints={endpoints} />
            )}
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

const VISIBILITY_OPTIONS: {
  value: McpServerVisibility;
  label: string;
  description: string;
  dotClass: string;
  hoverDotClass: string;
}[] = [
  {
    value: "disabled",
    label: "Disabled",
    description: "The server is offline.",
    dotClass: "bg-amber-400",
    hoverDotClass: "group-hover:bg-amber-400",
  },
  {
    value: "private",
    label: "Private",
    description: "Requires Gram platform authentication.",
    dotClass: "bg-blue-400",
    hoverDotClass: "group-hover:bg-blue-400",
  },
  {
    value: "public",
    label: "Public",
    description: "Relies solely on the server's configured authentication.",
    dotClass: "bg-green-400",
    hoverDotClass: "group-hover:bg-green-400",
  },
];

function MCPServerStatusDropdown({ server }: { server: McpServer }) {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const update = useUpdateMcpServerMutation();

  const handleSelect = async (next: McpServerVisibility) => {
    if (next === server.visibility) return;
    try {
      await update.mutateAsync({
        request: {
          updateMcpServerForm: {
            id: server.id,
            name: server.name ?? undefined,
            remoteMcpServerId: server.remoteMcpServerId ?? undefined,
            toolsetId: server.toolsetId ?? undefined,
            environmentId: server.environmentId ?? undefined,
            externalOauthServerId: server.externalOauthServerId ?? undefined,
            oauthProxyServerId: server.oauthProxyServerId ?? undefined,
            visibility: next,
          },
        },
      });
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success(
        next === "disabled"
          ? "MCP server disabled"
          : next === "public"
            ? "MCP server set to public"
            : "MCP server set to private",
      );
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to update server visibility";
      toast.error(message);
    }
  };

  const currentLabel =
    server.visibility === "disabled"
      ? "Disabled"
      : server.visibility === "public"
        ? "Public"
        : "Private";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={!canWrite || update.isPending}>
        <Button variant="primary" disabled={!canWrite || update.isPending}>
          <Button.Text>{currentLabel}</Button.Text>
          <Button.RightIcon>
            <ChevronDown className="h-4 w-4" />
          </Button.RightIcon>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-[320px] p-1">
        {VISIBILITY_OPTIONS.map((option) => (
          <DropdownMenuItem
            key={option.value}
            onSelect={() => handleSelect(option.value)}
            disabled={option.value === server.visibility}
            className="group flex cursor-pointer items-start gap-2.5 rounded-md p-2"
          >
            <span
              className={cn(
                "mt-1 h-2 w-2 shrink-0 rounded-full transition-colors",
                option.value === server.visibility
                  ? option.dotClass
                  : cn("bg-muted", option.hoverDotClass),
              )}
            />
            <div className="flex-1">
              <span className="block font-mono text-xs font-semibold tracking-wide uppercase">
                {option.label}
              </span>
              <span className="text-muted-foreground text-xs">
                {option.description}
              </span>
            </div>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function MCPServerHero({ server }: { server: McpServer | undefined }) {
  const enabled = server?.visibility !== "disabled";
  const isPublic = server?.visibility === "public";
  // The "Remote MCP" badge is keyed off the backing kind so it stays accurate
  // once toolset-backed mcp_servers also flow through this page (AGE-1902).
  const isRemoteBacked = !!server?.remoteMcpServerId;
  return (
    <DetailHero actions={server && <MCPServerStatusDropdown server={server} />}>
      <Stack gap={2}>
        <Stack direction="horizontal" gap={3} align="center">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-violet-500/10 dark:bg-violet-500/20">
            <Network className="h-5 w-5 text-violet-600 dark:text-violet-400" />
          </div>
          <Heading variant="h1" className="break-all normal-case">
            {server?.name || "MCP Server"}
          </Heading>
          {isRemoteBacked && (
            <Badge variant="neutral">
              <Badge.Text>Remote MCP</Badge.Text>
            </Badge>
          )}
        </Stack>
        <MCPStatusIndicator mcpEnabled={enabled} mcpIsPublic={isPublic} />
      </Stack>
    </DetailHero>
  );
}

function OverviewTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
  onShowEndpoints,
}: {
  mcpServer: McpServer | undefined;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  onShowEndpoints: () => void;
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      <InstallPagesSection
        endpoints={endpoints}
        isLoading={isLoadingEndpoints}
        onShowEndpoints={onShowEndpoints}
      />
      {mcpServer && <SourcesSection mcpServer={mcpServer} />}

      {/* TODO(AGE-2239): wire the install-page branding affordance in once
          mcp_metadata learns about mcp_server_id. */}
    </div>
  );
}

function EndpointsTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}) {
  const { domains } = useCustomDomains();

  const platformEndpoint = useMemo(
    () => endpoints.find((e) => !e.customDomainId),
    [endpoints],
  );
  const customDomainEndpoints = useMemo(
    () => endpoints.filter((e) => !!e.customDomainId),
    [endpoints],
  );

  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      <PlatformEndpointSurface
        mcpServer={mcpServer}
        endpoint={platformEndpoint}
        isLoading={isLoadingEndpoints}
      />
      <CustomDomainEndpointsSurface
        mcpServer={mcpServer}
        endpoints={customDomainEndpoints}
        domains={domains}
        isLoading={isLoadingEndpoints}
      />
    </div>
  );
}

function InstallPagesSection({
  endpoints,
  isLoading,
  onShowEndpoints,
}: {
  endpoints: McpEndpoint[];
  isLoading: boolean;
  onShowEndpoints: () => void;
}) {
  // Custom-domain endpoints render first so the more prominent customer-facing
  // URLs sit above the platform-hosted fallback.
  const sortedEndpoints = useMemo(
    () =>
      [...endpoints].sort((a, b) => {
        const aCustom = a.customDomainId ? 1 : 0;
        const bCustom = b.customDomainId ? 1 : 0;
        return bCustom - aCustom;
      }),
    [endpoints],
  );

  return (
    <section>
      <Heading variant="h4" className="mb-3">
        Client Install
      </Heading>
      <Type muted small className="mb-4">
        Share this page with your users to give simple instructions for getting
        started with your MCP in their client like Cursor or Claude Desktop.
      </Type>
      {isLoading ? (
        <Type muted small>
          Loading endpoints…
        </Type>
      ) : sortedEndpoints.length === 0 ? (
        <Stack gap={2}>
          <Type muted small>
            No endpoints configured yet.
          </Type>
          <div>
            <Button variant="secondary" onClick={onShowEndpoints}>
              <Button.LeftIcon>
                <Plus className="size-4" />
              </Button.LeftIcon>
              <Button.Text>Add Endpoint</Button.Text>
            </Button>
          </div>
        </Stack>
      ) : (
        <Stack gap={3}>
          {sortedEndpoints.map((endpoint) => (
            <InstallPageRow key={endpoint.id} endpoint={endpoint} />
          ))}
        </Stack>
      )}
    </section>
  );
}

function InstallPageRow({ endpoint }: { endpoint: McpEndpoint }) {
  const { installPageUrl } = useMcpEndpointUrl(endpoint);

  return installPageUrl ? (
    <CodeBlock copyable>{installPageUrl}</CodeBlock>
  ) : (
    <Type muted small>
      URL unavailable (custom domain still resolving).
    </Type>
  );
}

function SourcesSection({ mcpServer }: { mcpServer: McpServer }) {
  const isRemoteBacked = !!mcpServer.remoteMcpServerId;
  const isToolsetBacked = !!mcpServer.toolsetId;

  if (!isRemoteBacked && !isToolsetBacked) {
    return null;
  }

  return (
    <section>
      <Heading variant="h4" className="mb-3">
        Sources
      </Heading>
      <Type muted small className="mb-4">
        {isRemoteBacked
          ? "This MCP server is backed by a remote MCP server."
          : "This MCP server is backed by built sources."}
      </Type>
      {isRemoteBacked && mcpServer.remoteMcpServerId && (
        <RemoteSourceCard remoteMcpServerId={mcpServer.remoteMcpServerId} />
      )}
      {isToolsetBacked && mcpServer.toolsetId && (
        <ToolsetSourceCard toolsetId={mcpServer.toolsetId} />
      )}
    </section>
  );
}

function RemoteSourceCard({
  remoteMcpServerId,
}: {
  remoteMcpServerId: string;
}) {
  const routes = useRoutes();
  const { data: remoteMcpServer, isLoading } = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { throwOnError: false },
  );

  if (isLoading || !remoteMcpServer) {
    return (
      <SourceSkeletonCard
        icon={<Network className="text-muted-foreground h-8 w-8" />}
      />
    );
  }

  const trimmedName = remoteMcpServer.name?.trim();
  const urlDisplay = formatRemoteMcpUrlForDisplay(remoteMcpServer.url);

  return (
    <routes.sources.source.Link
      params={["remotemcp", remoteMcpRouteParam(remoteMcpServer)]}
      className="block hover:no-underline"
    >
      <SourceCardBody
        icon={<Network className="text-muted-foreground h-8 w-8" />}
        title={trimmedName || urlDisplay}
        subtitle={trimmedName ? urlDisplay : undefined}
        badgeLabel="Remote MCP"
      />
    </routes.sources.source.Link>
  );
}

function ToolsetSourceCard({ toolsetId }: { toolsetId: string }) {
  const routes = useRoutes();
  const { data: toolsetsResult, isLoading } = useListToolsets();
  const toolset = toolsetsResult?.toolsets.find(
    (t: ToolsetEntry) => t.id === toolsetId,
  );

  if (isLoading || !toolset) {
    return (
      <SourceSkeletonCard
        icon={<Wrench className="text-muted-foreground h-8 w-8" />}
      />
    );
  }

  const displayName = toolset.name?.trim() || toolset.slug;

  return (
    <routes.mcp.details.Link
      params={[toolset.slug]}
      className="block hover:no-underline"
    >
      <SourceCardBody
        icon={<Wrench className="text-muted-foreground h-8 w-8" />}
        title={displayName}
        badgeLabel="Toolset"
      />
    </routes.mcp.details.Link>
  );
}

function SourceCardBody({
  icon,
  title,
  subtitle,
  badgeLabel,
}: {
  icon: ReactNode;
  title: string;
  subtitle?: string;
  badgeLabel: string;
}) {
  return (
    <DotCard icon={icon}>
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <Type
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary truncate transition-colors"
            title={title}
          >
            {title}
          </Type>
          {subtitle && (
            <Type as="div" muted small className="truncate" title={subtitle}>
              {subtitle}
            </Type>
          )}
        </div>
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <Badge variant="neutral">
          <Badge.Text>{badgeLabel}</Badge.Text>
        </Badge>
        <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
          <span>Open</span>
          <ArrowRight className="h-3.5 w-3.5" />
        </div>
      </div>
    </DotCard>
  );
}

function SourceSkeletonCard({ icon }: { icon: React.ReactNode }) {
  return (
    <DotCard icon={icon}>
      <div className="bg-muted mb-2 h-5 w-1/3 animate-pulse rounded" />
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="bg-muted h-5 w-20 animate-pulse rounded-full" />
        <div className="bg-muted h-4 w-12 animate-pulse rounded" />
      </div>
    </DotCard>
  );
}

function SettingsTab({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      <GeneralSection mcpServer={mcpServer} />
      {/* TODO(AGE-2238): wire the Publishing section in once collections
          attachments learn about mcp_server_id. */}
      <DangerZoneSection mcpServer={mcpServer} endpoints={endpoints} />
    </div>
  );
}

function GeneralSection({ mcpServer }: { mcpServer: McpServer }) {
  const [nameDraft, setNameDraft] = useState(mcpServer.name ?? "");

  // Re-sync draft when the upstream record changes (e.g. another tab edited
  // it or a refetch landed). Without this a stale draft survives the refetch.
  useEffect(() => {
    setNameDraft(mcpServer.name ?? "");
  }, [mcpServer.id, mcpServer.name]);

  const queryClient = useQueryClient();
  const update = useUpdateMcpServerMutation();
  const navigate = useNavigate();
  const routes = useRoutes();

  const trimmedDraft = nameDraft.trim();
  const dirty = trimmedDraft !== (mcpServer.name ?? "").trim();
  const saveDisabled = !dirty || trimmedDraft === "" || update.isPending;

  const handleSave = async () => {
    try {
      const updated = await update.mutateAsync({
        request: {
          updateMcpServerForm: {
            id: mcpServer.id,
            name: trimmedDraft,
            remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
            toolsetId: mcpServer.toolsetId ?? undefined,
            environmentId: mcpServer.environmentId ?? undefined,
            externalOauthServerId: mcpServer.externalOauthServerId ?? undefined,
            oauthProxyServerId: mcpServer.oauthProxyServerId ?? undefined,
            visibility: mcpServer.visibility,
          },
        },
      });
      // The server recomputes slug on every update, so a name change produces
      // a new slug. Replace the route param with the new slug *before*
      // invalidating queries so the refetch uses the new lookup args and the
      // page-level not-found guard doesn't bounce the user back to /mcp.
      const nextParam = mcpServerRouteParam(updated);
      navigate(routes.mcp.x.href(nextParam), { replace: true });
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("MCP server updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update MCP server";
      toast.error(message);
    }
  };

  return (
    <div className="space-y-6">
      <Heading variant="h4">General</Heading>

      <Block label="Display name" className="p-0">
        <BlockInner>
          <Stack direction="horizontal" align="center" gap={2}>
            <Input
              value={nameDraft}
              onChange={(value) => setNameDraft(value)}
              placeholder="My MCP server"
            />
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="primary"
                size="md"
                disabled={saveDisabled}
                onClick={handleSave}
              >
                {update.isPending ? (
                  <>
                    <Button.LeftIcon>
                      <Loader2 className="size-4 animate-spin" />
                    </Button.LeftIcon>
                    <Button.Text>Saving</Button.Text>
                  </>
                ) : (
                  <Button.Text>Save</Button.Text>
                )}
              </Button>
            </RequireScope>
          </Stack>
        </BlockInner>
      </Block>
      {update.isError && (
        <Alert variant="error" dismissible={false}>
          {update.error.message}
        </Alert>
      )}
    </div>
  );
}

function PlatformEndpointSurface({
  mcpServer,
  endpoint,
  isLoading,
}: {
  mcpServer: McpServer;
  endpoint: McpEndpoint | undefined;
  isLoading: boolean;
}) {
  const [adding, setAdding] = useState(false);

  return (
    <section>
      <Heading variant="h4" className={endpoint ? "mb-1" : "mb-4"}>
        Platform endpoint
      </Heading>
      {endpoint && (
        <Type muted small className="mb-4">
          Optional platform-hosted path. Remove to access this server only
          through custom domain paths.
        </Type>
      )}
      {isLoading ? (
        <Type muted small>
          Loading…
        </Type>
      ) : endpoint ? (
        <EndpointRow mcpServer={mcpServer} endpoint={endpoint} />
      ) : adding ? (
        <NewEndpointRow
          mcpServer={mcpServer}
          customDomainId={null}
          onClose={() => setAdding(false)}
        />
      ) : (
        <RequireScope scope="mcp:write" level="component">
          <Button variant="secondary" onClick={() => setAdding(true)}>
            <Button.LeftIcon>
              <Plus className="size-4" />
            </Button.LeftIcon>
            <Button.Text>Add platform endpoint</Button.Text>
          </Button>
        </RequireScope>
      )}
    </section>
  );
}

function CustomDomainEndpointsSurface({
  mcpServer,
  endpoints,
  domains,
  isLoading,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  domains: Array<CustomDomain | undefined>;
  isLoading: boolean;
}) {
  const [adding, setAdding] = useState(false);
  const orgRoutes = useOrgRoutes();
  const { hasScope } = useRBAC();
  const canManageDomains = hasScope("org:admin");

  const availableDomains = domains.filter((d): d is CustomDomain => d != null);

  return (
    <section>
      <Heading variant="h4" className="mb-4">
        Custom domain endpoints
      </Heading>
      {isLoading ? (
        <Type muted small>
          Loading…
        </Type>
      ) : (
        <Stack gap={3}>
          {endpoints.map((endpoint) => (
            <EndpointRow
              key={endpoint.id}
              mcpServer={mcpServer}
              endpoint={endpoint}
              domains={availableDomains}
            />
          ))}
          {adding && (
            <NewCustomDomainEndpointRow
              mcpServer={mcpServer}
              domains={availableDomains}
              onClose={() => setAdding(false)}
            />
          )}
          {availableDomains.length === 0 ? (
            <Stack gap={2}>
              <Type muted small>
                {canManageDomains
                  ? "No custom domains configured for this organization yet."
                  : "No custom domains configured for this organization yet. Contact an organization administrator to set one up."}
              </Type>
              {canManageDomains && (
                <div>
                  <Button
                    variant="secondary"
                    onClick={() => orgRoutes.domains.goTo()}
                  >
                    <Button.LeftIcon>
                      <Plus className="size-4" />
                    </Button.LeftIcon>
                    <Button.Text>Add Custom Domain</Button.Text>
                  </Button>
                </div>
              )}
            </Stack>
          ) : (
            !adding && (
              <RequireScope scope="mcp:write" level="component">
                <Button variant="secondary" onClick={() => setAdding(true)}>
                  <Button.LeftIcon>
                    <Plus className="size-4" />
                  </Button.LeftIcon>
                  <Button.Text>Add endpoint</Button.Text>
                </Button>
              </RequireScope>
            )
          )}
        </Stack>
      )}
    </section>
  );
}

function EndpointRow({
  mcpServer,
  endpoint,
  domains,
}: {
  mcpServer: McpServer;
  endpoint: McpEndpoint;
  domains?: CustomDomain[];
}) {
  const [editing, setEditing] = useState(false);
  const [slugDraft, setSlugDraft] = useState(endpoint.slug);
  const { orgSlug } = useSlugs();
  const requiredPrefix =
    !endpoint.customDomainId && orgSlug ? `${orgSlug}-` : undefined;

  useEffect(() => {
    setSlugDraft(endpoint.slug);
  }, [endpoint.slug]);

  const queryClient = useQueryClient();
  const update = useUpdateMcpEndpointMutation();
  const remove = useDeleteMcpEndpointMutation();
  const slugError = useMcpEndpointSlugValidation(
    slugDraft.trim(),
    endpoint.customDomainId ?? null,
    endpoint.slug,
  );

  const dirty = slugDraft.trim() !== endpoint.slug;

  const customDomainLabel =
    endpoint.customDomainId &&
    domains?.find((d) => d.id === endpoint.customDomainId)?.domain;

  const handleSave = async () => {
    try {
      await update.mutateAsync({
        request: {
          updateMcpEndpointForm: {
            id: endpoint.id,
            mcpServerId: mcpServer.id,
            slug: slugDraft.trim(),
            customDomainId: endpoint.customDomainId ?? undefined,
          },
        },
      });
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Endpoint updated");
      setEditing(false);
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update endpoint";
      toast.error(message);
    }
  };

  const handleDelete = async () => {
    try {
      await remove.mutateAsync({ request: { id: endpoint.id } });
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Endpoint removed");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to delete endpoint";
      toast.error(message);
    }
  };

  const urlPrefix = customDomainLabel
    ? `https://${customDomainLabel}/x/mcp/`
    : `${getServerURL()}/x/mcp/`;

  return (
    <div className="rounded-md border p-3">
      <Stack direction="horizontal" gap={2} align="center">
        <Stack gap={0} className="min-w-0 flex-1">
          {editing ? (
            <Stack direction="horizontal" align="center">
              <Type muted mono variant="small">
                {urlPrefix}
              </Type>
              <Input
                value={slugDraft}
                onChange={(value) => setSlugDraft(value)}
                requiredPrefix={requiredPrefix}
              />
            </Stack>
          ) : (
            <Stack direction="horizontal" align="center" gap={0}>
              <Type muted mono variant="small">
                {urlPrefix}
              </Type>
              <Type small className="truncate font-mono">
                {endpoint.slug}
              </Type>
            </Stack>
          )}
        </Stack>
        <RequireScope scope="mcp:write" level="component">
          {editing ? (
            <>
              <Button
                size="md"
                variant="primary"
                disabled={!dirty || !!slugError || update.isPending}
                onClick={handleSave}
              >
                <Button.Text>Save</Button.Text>
              </Button>
              <Button
                size="md"
                variant="secondary"
                disabled={update.isPending}
                onClick={() => {
                  setSlugDraft(endpoint.slug);
                  setEditing(false);
                }}
              >
                <Button.Text>Cancel</Button.Text>
              </Button>
            </>
          ) : (
            <>
              <Button
                size="md"
                variant="secondary"
                onClick={() => setEditing(true)}
              >
                <Button.Text>Edit</Button.Text>
              </Button>
              <Button
                size="md"
                variant="destructive-secondary"
                disabled={remove.isPending}
                onClick={handleDelete}
              >
                <Button.LeftIcon>
                  <Trash2 className="size-4" />
                </Button.LeftIcon>
                <Button.Text>Delete</Button.Text>
              </Button>
            </>
          )}
        </RequireScope>
      </Stack>
      {editing && slugError && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {slugError}
        </Alert>
      )}
      {update.isError && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {update.error.message}
        </Alert>
      )}
    </div>
  );
}

function NewEndpointRow({
  mcpServer,
  customDomainId,
  onClose,
}: {
  mcpServer: McpServer;
  customDomainId: string | null;
  onClose: () => void;
}) {
  const [slug, setSlug] = useState("");
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const { orgSlug } = useSlugs();
  const requiredPrefix = !customDomainId && orgSlug ? `${orgSlug}-` : undefined;
  const slugError = useMcpEndpointSlugValidation(slug.trim(), customDomainId);

  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const handleCreate = async () => {
    const trimmed = slug.trim();
    if (!trimmed || slugError) return;
    setSubmitting(true);
    setErrorMsg(null);
    try {
      await client.mcpEndpoints.create({
        createMcpEndpointForm: {
          mcpServerId: mcpServer.id,
          slug: trimmed,
          customDomainId: customDomainId ?? undefined,
        },
      });
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Endpoint added");
      onClose();
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to add endpoint";
      setErrorMsg(message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="rounded-md border p-3">
      <Stack direction="horizontal" gap={2} align="center">
        <Stack direction="horizontal" align="center" className="min-w-0 flex-1">
          {!customDomainId && (
            <Type muted mono variant="small">
              {`${getServerURL()}/x/mcp/`}
            </Type>
          )}
          <Input
            value={slug}
            onChange={(value) => setSlug(value)}
            placeholder="my-endpoint"
            requiredPrefix={requiredPrefix}
          />
        </Stack>
        <Button
          size="md"
          variant="primary"
          disabled={!slug.trim() || !!slugError || submitting}
          onClick={handleCreate}
        >
          <Button.Text>Add</Button.Text>
        </Button>
        <Button
          size="md"
          variant="secondary"
          disabled={submitting}
          onClick={onClose}
        >
          <Button.Text>Cancel</Button.Text>
        </Button>
      </Stack>
      {slug.trim() && slugError && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {slugError}
        </Alert>
      )}
      {errorMsg && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {errorMsg}
        </Alert>
      )}
    </div>
  );
}

function NewCustomDomainEndpointRow({
  mcpServer,
  domains,
  onClose,
}: {
  mcpServer: McpServer;
  domains: CustomDomain[];
  onClose: () => void;
}) {
  const [domainId, setDomainId] = useState<string>(domains[0]?.id ?? "");
  const [slug, setSlug] = useState("");
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const slugError = useMcpEndpointSlugValidation(slug.trim(), domainId || null);

  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const handleCreate = async () => {
    const trimmed = slug.trim();
    if (!trimmed || !domainId || slugError) return;
    setSubmitting(true);
    setErrorMsg(null);
    try {
      await client.mcpEndpoints.create({
        createMcpEndpointForm: {
          mcpServerId: mcpServer.id,
          slug: trimmed,
          customDomainId: domainId,
        },
      });
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Endpoint added");
      onClose();
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to add endpoint";
      setErrorMsg(message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="rounded-md border p-3">
      <Stack direction="horizontal" gap={2} align="center">
        <Stack
          direction="horizontal"
          align="center"
          gap={1}
          className="min-w-0 flex-1"
        >
          <Type muted mono variant="small">
            https://
          </Type>
          <Select
            value={domainId}
            onValueChange={(value) => setDomainId(value)}
          >
            <SelectTrigger>
              <SelectValue placeholder="Custom domain" />
            </SelectTrigger>
            <SelectContent>
              {domains.map((domain) => (
                <SelectItem key={domain.id} value={domain.id}>
                  {domain.domain}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Type muted mono variant="small">
            /x/mcp/
          </Type>
          <Input
            value={slug}
            onChange={(value) => setSlug(value)}
            placeholder="my-endpoint"
          />
        </Stack>
        <Button
          size="md"
          variant="primary"
          disabled={!slug.trim() || !domainId || !!slugError || submitting}
          onClick={handleCreate}
        >
          <Button.Text>Add</Button.Text>
        </Button>
        <Button
          size="md"
          variant="secondary"
          disabled={submitting}
          onClick={onClose}
        >
          <Button.Text>Cancel</Button.Text>
        </Button>
      </Stack>
      {slug.trim() && slugError && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {slugError}
        </Alert>
      )}
      {errorMsg && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {errorMsg}
        </Alert>
      )}
    </div>
  );
}

function DangerZoneSection({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}) {
  const navigate = useNavigate();
  const routes = useRoutes();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  return (
    <div className="border-destructive/30 rounded-lg border p-6">
      <Type variant="subheading" className="text-destructive mb-1">
        Danger Zone
      </Type>
      <Type muted small className="mb-4">
        Deleting this MCP server also removes its endpoints. This action cannot
        be undone.
      </Type>
      <RequireScope scope="mcp:write" level="component">
        <Button
          variant="destructive-primary"
          size="md"
          onClick={() => setDeleteDialogOpen(true)}
        >
          <Button.LeftIcon>
            <Trash2 className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Delete MCP server</Button.Text>
        </Button>
      </RequireScope>

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <Dialog.Content className="max-w-2xl!">
          <DeleteMcpServerDialogContent
            mcpServer={mcpServer}
            endpoints={endpoints}
            onClose={() => setDeleteDialogOpen(false)}
            onSuccess={() => {
              setDeleteDialogOpen(false);
              navigate(routes.mcp.href());
            }}
          />
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

function DeleteMcpServerDialogContent({
  mcpServer,
  endpoints,
  onClose,
  onSuccess,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  onClose: () => void;
  onSuccess: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useDeleteMcpServerMutation();

  const handleConfirm = async () => {
    try {
      await remove.mutateAsync({ request: { id: mcpServer.id } });
      await Promise.all([
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
      toast.success("MCP server deleted");
      onSuccess();
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to delete MCP server";
      toast.error(message);
    }
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete this MCP server?</Dialog.Title>
      </Dialog.Header>
      <Stack gap={3}>
        <Type>
          This will soft-delete the MCP server <strong>{mcpServer.name}</strong>{" "}
          and the following endpoints. The action cannot be undone.
        </Type>
        {endpoints.length > 0 ? (
          <ul className="list-disc pl-6">
            {endpoints.map((endpoint) => (
              <li key={endpoint.id}>
                <Type small className="font-mono">
                  {endpoint.slug}
                  {endpoint.customDomainId
                    ? " (custom domain)"
                    : " (platform-hosted)"}
                </Type>
              </li>
            ))}
          </ul>
        ) : (
          <Type muted small>
            No endpoints are currently associated with this MCP server.
          </Type>
        )}
        {remove.isError && (
          <Alert variant="error" dismissible={false}>
            {remove.error.message}
          </Alert>
        )}
        <Stack direction="horizontal" gap={2}>
          <Button
            variant="destructive-primary"
            disabled={remove.isPending}
            onClick={handleConfirm}
          >
            {remove.isPending ? (
              <>
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
                <Button.Text>Deleting</Button.Text>
              </>
            ) : (
              <Button.Text>Delete MCP server</Button.Text>
            )}
          </Button>
          <Button
            variant="secondary"
            disabled={remove.isPending}
            onClick={onClose}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
        </Stack>
      </Stack>
    </>
  );
}
