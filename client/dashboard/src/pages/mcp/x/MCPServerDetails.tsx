import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { cn } from "@/lib/utils";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { getMcpServerArgs } from "@/lib/sources";
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown } from "lucide-react";
import { Navigate, useLocation, useParams } from "react-router";
import { toast } from "sonner";
import { MCPTeamAccessTab } from "../MCPTeamAccessTab";
import {
  activeTabFromPath,
  initialTabFromHash,
  isLegacyAuthenticationTabPath,
  mcpServerTabHref,
} from "./MCPServerDetailsRouting";
import { MCPOverviewTab } from "@/pages/mcp/overview/MCPOverviewTab";
import { ToolsTab } from "./tabs/ToolsTab";
import { MCP_AUTHENTICATION_SECTION_ID } from "./tabs/settings/sections/authentication/AuthenticationSection";
import { SettingsTab } from "./tabs/settings/SettingsTab";

const MCP_X_TAB_URLS = ["overview", "tools", "team-access", "settings"];

export default function MCPServerDetails(): JSX.Element {
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const location = useLocation();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;
  const idOrSlug = mcpServerSlug ?? "";
  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const legacyAuthenticationPath = isLegacyAuthenticationTabPath(
    location.pathname,
    idOrSlug,
  );

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

  const renderTabContent = () => {
    switch (activeTab) {
      case "overview":
        return (
          mcpServer &&
          mcpServer.slug && (
            <MCPOverviewTab
              server={{
                kind: "mcp-server",
                id: mcpServer.id,
                slug: mcpServer.slug,
                name: mcpServer.name ?? "MCP Server",
              }}
            />
          )
        );
      case "tools":
        return (
          mcpServer && (
            <ToolsTab
              mcpServer={mcpServer}
              endpoints={endpoints}
              isLoadingEndpoints={isLoadingEndpoints}
            />
          )
        );
      case "team-access":
        return (
          isRbacEnabled &&
          mcpServer && (
            <RequireScope scope="mcp:read" level="page">
              {/* mcp_servers-backed servers grant under the same `mcp:*`
                scope kind as toolset-backed ones (see selector.go), so
                MCPTeamAccessTab is reused as-is with the mcp_server's
                id as the resource id. No `tools` prop because the
                Remote MCP backend doesn't expose a Gram-side tool
                catalog. */}
              <MCPTeamAccessTab resourceId={mcpServer.id} />
            </RequireScope>
          )
        );
      case "settings":
        return (
          mcpServer && (
            <SettingsTab
              mcpServer={mcpServer}
              endpoints={endpoints}
              isLoadingEndpoints={isLoadingEndpoints}
            />
          )
        );
    }
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [idOrSlug]: mcpServer?.name || "MCP Server",
          }}
          skipSegments={[
            "x",
            // skipSegments matches by literal value, not position — if the
            // server's own slug happens to collide with a tab name (e.g. a
            // server slugged "settings"), guard against also skipping the
            // server's own breadcrumb crumb.
            ...MCP_X_TAB_URLS.filter((tab) => tab !== idOrSlug),
          ]}
        />
      </Page.Header>

      <Page.Body fullWidth className="gap-0">
        <div className="mx-auto w-full max-w-[1270px] flex-1">
          {renderTabContent()}
        </div>
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
    description: "This server is offline. No users can connect to it",
    dotClass: "bg-amber-400",
    hoverDotClass: "group-hover:bg-amber-400",
  },
  {
    value: "private",
    label: "Private",
    description: "The server serves traffic.",
    dotClass: "bg-blue-400",
    hoverDotClass: "group-hover:bg-blue-400",
  },
];

export function MCPServerStatusDropdown({
  server,
}: {
  server: McpServer;
}): JSX.Element {
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
        <button
          type="button"
          disabled={!canWrite || update.isPending}
          className="text-foreground hover:bg-muted trans border-border flex w-fit items-center gap-2 rounded-md border px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
        >
          <span
            className={cn(
              "h-2 w-2 shrink-0 rounded-full",
              VISIBILITY_OPTIONS.find(
                (option) => option.value === server.visibility,
              )?.dotClass ?? "bg-green-400",
            )}
          />
          {currentLabel}
          <ChevronDown className="text-muted-foreground h-3 w-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-[320px] p-1">
        {VISIBILITY_OPTIONS.map((option) => (
          <DropdownMenuItem
            key={option.value}
            onSelect={() => handleSelect(option.value)}
            className="group flex cursor-pointer items-start gap-2.5 rounded-md p-2"
          >
            {option.value === server.visibility ? (
              <span
                className={cn(
                  "mt-1 flex size-3.5 shrink-0 items-center justify-center rounded-full",
                  option.dotClass,
                )}
              >
                <Check
                  className="text-background h-2.5 w-2.5"
                  strokeWidth={4}
                />
              </span>
            ) : (
              <span
                className={cn(
                  "mt-1 size-3.5 shrink-0 rounded-full transition-colors",
                  "bg-muted",
                  option.hoverDotClass,
                )}
              />
            )}
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
