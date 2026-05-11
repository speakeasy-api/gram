import { MCPStatusIndicator } from "@/components/mcp/MCPStatusIndicator";
import { Page } from "@/components/page-layout";
import { DetailHero } from "@/components/detail-hero";
import { Heading } from "@/components/ui/heading";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { RequireScope } from "@/components/require-scope";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { getMcpServerArgs } from "@/lib/sources";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components";
import {
  invalidateAllGetMcpServer,
  invalidateAllMcpServers,
  useGetMcpServer,
  useMcpEndpoints,
  useUpdateMcpServerMutation,
} from "@gram/client/react-query/index.js";
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Stack,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Network } from "lucide-react";
import { useState } from "react";
import { Navigate, useParams } from "react-router";
import { toast } from "sonner";
import { MCPTeamAccessTab } from "../MCPTeamAccessTab";
import { EndpointsTab } from "./tabs/EndpointsTab";
import { OverviewTab } from "./tabs/OverviewTab";
import { SettingsTab } from "./tabs/SettingsTab";

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
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update server visibility",
      );
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
          toolsetId: server.toolsetId ?? undefined,
          environmentId: server.environmentId ?? undefined,
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
