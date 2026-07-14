import { MCPStatusIndicator } from "@/components/mcp/MCPStatusIndicator";
import { Page } from "@/components/page-layout";
import { DetailLayout } from "@/components/layouts/detail-layout";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { RequireScope } from "@/components/require-scope";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { getMcpServerArgs } from "@/lib/sources";
import { toastError } from "@/lib/toast-error";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import {
  invalidateAllGetMcpServer,
  useGetMcpServer,
} from "@gram/client/react-query/getMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import {
  Link,
  Navigate,
  useLocation,
  useNavigate,
  useParams,
} from "react-router";
import { toast } from "sonner";
import { MCPTeamAccessTab } from "../MCPTeamAccessTab";
import {
  activeTabFromPath,
  initialTabFromHash,
  isLegacyAuthenticationTabPath,
  type TabValue,
} from "./MCPServerDetailsRouting";
import { AnalyticsTab } from "./tabs/AnalyticsTab";
import { OverviewTab } from "./tabs/OverviewTab";
import { ToolsTab } from "./tabs/ToolsTab";
import { MCP_AUTHENTICATION_SECTION_ID } from "./tabs/settings/sections/authentication/AuthenticationSection";
import { MCP_SERVER_URL_SECTION_ID } from "./tabs/settings/sections/ServerUrlSection";
import { SettingsTab } from "./tabs/settings/SettingsTab";

function mcpServerTabHref(
  routes: ReturnType<typeof useRoutes>,
  mcpServerSlug: string,
  tab: TabValue,
): string {
  switch (tab) {
    case "overview":
      return routes.mcp.x.overview.href(mcpServerSlug);
    case "tools":
      return routes.mcp.x.tools.href(mcpServerSlug);
    case "analytics":
      return routes.mcp.x.analytics.href(mcpServerSlug);
    case "team-access":
      return routes.mcp.x.teamAccess.href(mcpServerSlug);
    case "settings":
      return routes.mcp.x.settings.href(mcpServerSlug);
  }
}

export default function MCPServerDetails(): JSX.Element {
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const location = useLocation();
  const navigate = useNavigate();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;
  const idOrSlug = mcpServerSlug ?? "";
  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const legacyAuthenticationPath = isLegacyAuthenticationTabPath(
    location.pathname,
    idOrSlug,
  );

  const handleShowServerUrlSettings = () => {
    void navigate(
      `${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_SERVER_URL_SECTION_ID}`,
    );
  };

  const handleShowAuthentication = () => {
    void navigate(
      `${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AUTHENTICATION_SECTION_ID}`,
    );
  };

  const {
    data: mcpServer,
    isLoading,
    isError,
  } = useGetMcpServer(getMcpServerArgs(idOrSlug), undefined, {
    enabled: idOrSlug !== "",
  });

  const mcpServerId = mcpServer?.id ?? "";

  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ mcpServerId }, undefined, {
      enabled: mcpServerId !== "",
    });
  const endpoints = endpointsResult?.mcpEndpoints ?? [];

  if (!idOrSlug) {
    return <Navigate to={routes.mcp.href()} replace />;
  }
  if (isError || (!isLoading && !mcpServer)) {
    return <Navigate to={routes.mcp.href()} replace />;
  }
  if (legacyAuthenticationPath) {
    return (
      <Navigate
        to={`${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AUTHENTICATION_SECTION_ID}`}
        replace
      />
    );
  }
  if (!activeTab) {
    const initialTab = initialTabFromHash(location.hash, isRbacEnabled);
    const hash =
      location.hash === `#${MCP_AUTHENTICATION_SECTION_ID}`
        ? `#${MCP_AUTHENTICATION_SECTION_ID}`
        : "";

    return (
      <Navigate
        to={`${mcpServerTabHref(routes, idOrSlug, initialTab)}${hash}`}
        replace
      />
    );
  }
  if (activeTab === "team-access" && !isRbacEnabled) {
    return (
      <Navigate to={mcpServerTabHref(routes, idOrSlug, "overview")} replace />
    );
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

      <Page.Body>
        <DetailLayout>
          <MCPServerHeader server={mcpServer} />

          <Tabs value={activeTab} className="flex w-full flex-1 flex-col">
            <DetailLayout.Tabs>
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <PageTabsTrigger value="overview" asChild>
                  <Link to={mcpServerTabHref(routes, idOrSlug, "overview")}>
                    Overview
                  </Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="tools" asChild>
                  <Link to={mcpServerTabHref(routes, idOrSlug, "tools")}>
                    Tools
                  </Link>
                </PageTabsTrigger>
                <PageTabsTrigger value="analytics" asChild>
                  <Link to={mcpServerTabHref(routes, idOrSlug, "analytics")}>
                    Analytics
                  </Link>
                </PageTabsTrigger>
                {isRbacEnabled && (
                  <PageTabsTrigger value="team-access" asChild>
                    <Link
                      to={mcpServerTabHref(routes, idOrSlug, "team-access")}
                    >
                      Team Access
                    </Link>
                  </PageTabsTrigger>
                )}
                <PageTabsTrigger value="settings" asChild>
                  <Link to={mcpServerTabHref(routes, idOrSlug, "settings")}>
                    Settings
                  </Link>
                </PageTabsTrigger>
              </TabsList>
            </DetailLayout.Tabs>

            <DetailLayout.Content>
              <DetailLayout.Main>
                <TabsContent
                  value="overview"
                  className="mt-0 w-full data-[state=inactive]:hidden"
                >
                  <OverviewTab
                    mcpServer={mcpServer}
                    endpoints={endpoints}
                    isLoadingEndpoints={isLoadingEndpoints}
                    onShowEndpoints={handleShowServerUrlSettings}
                    onShowAuthentication={handleShowAuthentication}
                  />
                </TabsContent>

                <TabsContent
                  value="tools"
                  className="mt-0 w-full data-[state=inactive]:hidden"
                >
                  {mcpServer && (
                    <ToolsTab
                      mcpServer={mcpServer}
                      endpoints={endpoints}
                      isLoadingEndpoints={isLoadingEndpoints}
                    />
                  )}
                </TabsContent>

                <TabsContent
                  value="analytics"
                  className="mt-0 w-full data-[state=inactive]:hidden"
                >
                  <AnalyticsTab mcpServer={mcpServer} />
                </TabsContent>

                {isRbacEnabled && mcpServer && (
                  <TabsContent
                    value="team-access"
                    className="mt-0 w-full data-[state=inactive]:hidden"
                  >
                    <RequireScope scope="mcp:read" level="page">
                      {/* mcp_servers-backed servers grant under the same
                        `mcp:*` scope kind as toolset-backed ones (see
                        selector.go), so MCPTeamAccessTab is reused as-is
                        with the mcp_server's id as the resource id. No
                        `tools` prop because the Remote MCP backend doesn't
                        expose a Gram-side tool catalog. */}
                      <MCPTeamAccessTab resourceId={mcpServer.id} />
                    </RequireScope>
                  </TabsContent>
                )}

                <TabsContent
                  value="settings"
                  className="mt-0 w-full data-[state=inactive]:hidden"
                >
                  {mcpServer && (
                    <SettingsTab
                      mcpServer={mcpServer}
                      endpoints={endpoints}
                      isLoadingEndpoints={isLoadingEndpoints}
                    />
                  )}
                </TabsContent>
              </DetailLayout.Main>
            </DetailLayout.Content>
          </Tabs>
        </DetailLayout>
      </Page.Body>
    </Page>
  );
}

// The dropdown only offers the two states that gate whether the server
// serves traffic. Any other stored visibility values render their label via
// currentLabel below.
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
    dotClass: "bg-warning-default",
    hoverDotClass: "group-hover:bg-warning-default",
  },
  {
    value: "private",
    label: "Private",
    description: "The server serves traffic.",
    dotClass: "bg-information-default",
    hoverDotClass: "group-hover:bg-information-default",
  },
];

function MCPServerStatusDropdown({ server }: { server: McpServer }) {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const update = useUpdateMcpServerMutation({
    onSuccess: async (_data, variables) => {
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      const next = variables.request.updateMcpServerForm.visibility;
      toast.success(
        next === "disabled"
          ? "MCP server disabled"
          : next === "public"
            ? "MCP server set to public"
            : "MCP server set to private",
      );
    },
    onError: (error) => {
      toastError(error, "Failed to update server visibility");
    },
  });

  const handleSelect = (next: McpServerVisibility) => {
    if (next === server.visibility) return;
    update.mutate({
      request: {
        updateMcpServerForm: {
          id: server.id,
          name: server.name ?? undefined,
          remoteMcpServerId: server.remoteMcpServerId ?? undefined,
          tunneledMcpServerId: server.tunneledMcpServerId ?? undefined,
          toolsetId: server.toolsetId ?? undefined,
          environmentId: server.environmentId ?? undefined,
          // updateMcpServer is a full-record replace for the optional UUID
          // references. Forwarding them keeps stored values intact across a
          // visibility-only update.
          userSessionIssuerId: server.userSessionIssuerId ?? undefined,
          toolVariationsGroupId: server.toolVariationsGroupId ?? undefined,
          visibility: next,
        },
      },
    });
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
            className="group flex cursor-pointer items-start gap-2.5 p-2"
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
              <Type
                mono
                small
                as="span"
                className="block font-semibold tracking-wide uppercase"
              >
                {option.label}
              </Type>
              <Type muted small as="span">
                {option.description}
              </Type>
            </div>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function MCPServerHeader({ server }: { server: McpServer | undefined }) {
  const enabled = server?.visibility !== "disabled";
  const isPublic = server?.visibility === "public";
  const isHostedServer =
    !!server?.remoteMcpServerId || !!server?.tunneledMcpServerId;
  return (
    <DetailLayout.Header
      eyebrow="MCP Server"
      title={
        <span className="inline-flex items-center gap-3 break-all">
          {server?.name || "MCP Server"}
          {isHostedServer && (
            <Badge variant="neutral">
              <Badge.Text>Hosted MCP</Badge.Text>
            </Badge>
          )}
        </span>
      }
      subtitle={
        <MCPStatusIndicator mcpEnabled={enabled} mcpIsPublic={isPublic} />
      }
      actions={server && <MCPServerStatusDropdown server={server} />}
    />
  );
}
