import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { cn } from "@/lib/utils";
import { useRBAC } from "@/hooks/useRBAC";
import { useTabScrollReset } from "@/hooks/useTabScrollReset";
import { getMcpServerArgs } from "@/lib/sources";
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
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import { invalidateAllPlugins } from "@gram/client/react-query/plugins";
import { invalidateAllPublishStatus } from "@gram/client/react-query/publishStatus";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown } from "lucide-react";
import { Navigate, useLocation, useParams } from "react-router";
import { toast } from "sonner";
import { MCPTeamAccessTab } from "../MCPTeamAccessTab";
import {
  activeTabFromPath,
  initialTabFromHash,
  isLegacyAuthenticationTabPath,
  isLegacyToolsTabPath,
  mcpServerTabHref,
  MCP_SERVER_TAB_URLS,
} from "./MCPServerDetailsRouting";
import { MCPOverviewTab } from "@/pages/mcp/overview/MCPOverviewTab";
import { InspectTab } from "./tabs/InspectTab";
import { MCP_AUTHENTICATION_SECTION_ID } from "./tabs/settings/sections/authentication/AuthenticationSection";
import { ClientsAndSessionsTab } from "@/components/sessions/ClientsAndSessionsTab";
import { SettingsTab } from "./tabs/settings/SettingsTab";
import { UnproxiedMcpOverviewTab } from "./tabs/UnproxiedMcpOverviewTab";

export default function MCPServerDetails(): JSX.Element {
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const location = useLocation();
  const routes = useRoutes();
  const idOrSlug = mcpServerSlug ?? "";
  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const tabContentRef = useTabScrollReset(activeTab);
  const legacyAuthenticationPath = isLegacyAuthenticationTabPath(
    location.pathname,
    idOrSlug,
  );
  const legacyToolsPath = isLegacyToolsTabPath(location.pathname, idOrSlug);

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
  if (legacyToolsPath) {
    return (
      <Navigate to={mcpServerTabHref(routes, idOrSlug, "inspect")} replace />
    );
  }
  // Inspect and Clients and Sessions are hidden from the nav for unproxied
  // servers (see McpServerXSidebarNav) but the routes still resolve, so bounce
  // anyone who lands on them directly (old links, the legacy /tools redirect
  // above) back to Overview instead of rendering a tab with nothing to show.
  if (
    (activeTab === "inspect" || activeTab === "sessions") &&
    mcpServer?.unproxiedMcpServerId
  ) {
    return (
      <Navigate to={mcpServerTabHref(routes, idOrSlug, "overview")} replace />
    );
  }
  if (!activeTab) {
    const initialTab = initialTabFromHash(location.hash);
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
  const renderTabContent = () => {
    switch (activeTab) {
      case "overview":
        return (
          mcpServer &&
          (mcpServer.unproxiedMcpServerId ? (
            <UnproxiedMcpOverviewTab
              unproxiedMcpServerId={mcpServer.unproxiedMcpServerId}
              mcpServerId={mcpServer.id}
              mcpServerSlug={mcpServer.slug ?? ""}
              mcpServerName={mcpServer.name ?? "MCP Server"}
            />
          ) : (
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
          ))
        );
      case "inspect":
        return (
          mcpServer && (
            <InspectTab
              mcpServer={mcpServer}
              endpoints={endpoints}
              isLoadingEndpoints={isLoadingEndpoints}
            />
          )
        );
      case "team-access":
        return (
          mcpServer && (
            <RequireScope scope="org:read" level="page">
              <RequireScope
                scope="mcp:read"
                resourceId={mcpServer.id}
                level="page"
              >
                {/* mcp_servers-backed servers grant under the same `mcp:*`
                  scope kind as toolset-backed ones (see selector.go), so
                  MCPTeamAccessTab is reused as-is with the mcp_server's
                  id as the resource id. No `tools` prop because the
                  Remote MCP backend doesn't expose a Gram-side tool
                  catalog. */}
                <MCPTeamAccessTab resourceId={mcpServer.id} />
              </RequireScope>
            </RequireScope>
          )
        );
      case "sessions":
        return (
          mcpServer && (
            <RequireScope scope="project:read" level="page">
              <ClientsAndSessionsTab issuerId={mcpServer.userSessionIssuerId} />
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
            ...MCP_SERVER_TAB_URLS.filter((tab) => tab !== idOrSlug),
          ]}
        />
      </Page.Header>

      <Page.Body fullWidth className="gap-0">
        <div
          ref={tabContentRef}
          className="mx-auto w-full max-w-[1270px] flex-1"
        >
          {renderTabContent()}
        </div>
      </Page.Body>
    </Page>
  );
}

type VisibilityOption = {
  value: McpServerVisibility;
  label: string;
  description: string;
  dotClass: string;
  hoverDotClass: string;
};

const VISIBILITY_OPTIONS: VisibilityOption[] = [
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

// Public visibility is only offered for tunneled-backed servers, and only
// once the tunnel source owner has consented (double opt-in).
const PUBLIC_VISIBILITY_OPTION: VisibilityOption = {
  value: "public",
  label: "Public",
  description:
    "Anyone can connect anonymously — no login. Every tool is exposed to the public internet.",
  dotClass: "bg-green-400",
  hoverDotClass: "group-hover:bg-green-400",
};

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
        // Enabling a disabled server (e.g. disabled -> private) auto-attaches
        // it to the Default plugin server-side, which the plugin banner's
        // membership check and publish-freshness state need to pick up.
        invalidateAllPlugins(queryClient, { refetchType: "all" }),
        invalidateAllPublishStatus(queryClient, { refetchType: "all" }),
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
          unproxiedMcpServerId: server.unproxiedMcpServerId ?? undefined,
          environmentId: server.environmentId ?? undefined,
          // updateMcpServer is a full-record replace for the optional UUID
          // references. Forwarding them keeps stored values intact across a
          // visibility-only update.
          toolVariationsGroupId: server.toolVariationsGroupId ?? undefined,
          visibility: next,
        },
      },
    });
  };

  // Mapped explicitly rather than defaulting the tail to "Private". Upstream
  // servers are the one thing that default got exactly backwards: they are not
  // private, and they are not selectable from the dropdown below either (the
  // mode selector is AIM-27), so this is read-only display for a value only
  // the API can currently set.
  const currentLabel =
    server.visibility === "disabled"
      ? "Disabled"
      : server.visibility === "public"
        ? "Public"
        : server.visibility === "upstream"
          ? "Upstream auth"
          : "Private";

  const isTunneled = Boolean(server.tunneledMcpServerId);
  const { data: tunneledSource } = useGetTunneledMcpServer(
    getTunneledMcpServerArgs(server.tunneledMcpServerId ?? ""),
    undefined,
    { enabled: isTunneled },
  );
  const sourceAllowsPublic = tunneledSource?.allowPublic ?? false;

  const options = isTunneled
    ? [...VISIBILITY_OPTIONS, PUBLIC_VISIBILITY_OPTION]
    : VISIBILITY_OPTIONS;

  // A value with no option — today only "upstream" — must not inherit green,
  // which this file assigns to Public ("anyone can connect anonymously"). That
  // is the opposite of what an upstream server does.
  const currentDotClass =
    options.find((option) => option.value === server.visibility)?.dotClass ??
    "bg-muted-foreground/60";

  // Unproxied servers have no Gram-hosted endpoint for disabled/private to
  // gate — the vendor's own server is reachable regardless of this setting —
  // so there's nothing to toggle. Still show the record's actual stored
  // value (not a hardcoded "Public") so this can't drift from what Settings
  // and the readiness checklist report for the same server.
  if (server.unproxiedMcpServerId) {
    return (
      <span className="text-foreground border-border flex w-fit items-center gap-2 border px-3 py-1.5 text-sm font-medium">
        <span
          className={cn("h-2 w-2 shrink-0 rounded-full", currentDotClass)}
        />
        {currentLabel}
      </span>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={!canWrite || update.isPending}>
        <button
          type="button"
          disabled={!canWrite || update.isPending}
          className="text-foreground hover:bg-muted trans border-border flex w-fit items-center gap-2 border px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
        >
          <span
            className={cn("h-2 w-2 shrink-0 rounded-full", currentDotClass)}
          />
          {currentLabel}
          <ChevronDown className="text-muted-foreground h-3 w-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-[320px] p-1">
        {options.map((option) => {
          // Public is gated on the source's consent; render it disabled with
          // a hint rather than hiding it, so owners know the toggle exists.
          const publicBlocked =
            option.value === "public" && !sourceAllowsPublic;
          return (
            <DropdownMenuItem
              key={option.value}
              disabled={publicBlocked}
              onSelect={() => {
                if (publicBlocked) return;
                handleSelect(option.value);
              }}
              className="group flex cursor-pointer items-start gap-2.5 p-2 data-[disabled]:cursor-not-allowed data-[disabled]:opacity-60"
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
                  {publicBlocked
                    ? "Enable public access on the tunnel source first to allow anonymous serving."
                    : option.description}
                </span>
              </div>
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
