import {
  McpSidebarInfoLabel,
  McpSidebarNavShell,
  type McpSidebarNavItem,
} from "@/components/mcp-sidebar-nav-shell";
import { useExternalMcpOAuthConfigStatus } from "@/components/sources/sources-hooks";
import { CopyButton } from "@/components/ui/copy-button";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useToolset } from "@/hooks/toolTypes";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import {
  MCPStatusDropdown,
  RenameMCPServerButton,
} from "@/pages/mcp/MCPDetails";
import {
  activeTabFromPath,
  mcpDetailTabHref,
} from "@/pages/mcp/MCPDetailsRouting";
import { useRoutes } from "@/routes";
import {
  useGetMcpMetadata,
  useListEnvironments,
} from "@gram/client/react-query";
import {
  AlertTriangle,
  ArrowRight,
  Database,
  ExternalLink,
  Gauge,
  KeyRound,
  LayoutDashboard,
  MessageSquareText,
  Settings as SettingsIcon,
  Users,
  Wrench,
} from "lucide-react";
import * as React from "react";
import { useLocation, useParams } from "react-router";

export function McpDetailSidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const location = useLocation();
  const { toolsetSlug } = useParams<{ toolsetSlug: string }>();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;

  const { data: toolset } = useToolset(toolsetSlug);
  const { url: mcpUrl, installPageUrl } = useMcpUrl(toolset);
  const { data: environmentsData } = useListEnvironments();
  const { data: mcpMetadataData } = useGetMcpMetadata(
    { toolsetSlug: toolsetSlug ?? "" },
    undefined,
    { enabled: !!toolsetSlug, throwOnError: false },
  );
  const missingRequiredEnvVars = useMissingRequiredEnvVars(
    toolset,
    environmentsData?.environments ?? [],
    toolset?.defaultEnvironmentSlug || "default",
    mcpMetadataData?.metadata,
  );
  const oauthRequiredUnconfigured =
    useExternalMcpOAuthConfigStatus(toolsetSlug) === "required-unconfigured";

  if (!toolsetSlug) return null;

  const activeTab = activeTabFromPath(location.pathname, toolsetSlug);

  const items: McpSidebarNavItem[] = [
    {
      key: "overview",
      title: "Overview",
      Icon: LayoutDashboard,
      href: mcpDetailTabHref(routes, toolsetSlug, "overview"),
      active: activeTab === "overview",
    },
    {
      key: "tools",
      title: "Tools",
      Icon: Wrench,
      href: mcpDetailTabHref(routes, toolsetSlug, "tools"),
      active: activeTab === "tools",
    },
    {
      key: "authentication",
      title: "Authentication",
      Icon: KeyRound,
      titleNode: (
        <span className="flex items-center gap-1.5">
          Authentication
          {(missingRequiredEnvVars > 0 || oauthRequiredUnconfigured) && (
            <AlertTriangle className="text-warning h-3.5 w-3.5 shrink-0" />
          )}
        </span>
      ),
      href: mcpDetailTabHref(routes, toolsetSlug, "authentication"),
      active: activeTab === "authentication",
    },
    {
      key: "performance",
      title: "Performance",
      Icon: Gauge,
      href: mcpDetailTabHref(routes, toolsetSlug, "performance"),
      active: activeTab === "performance",
    },
    ...(isRbacEnabled
      ? [
          {
            key: "team-access",
            title: "Team Access",
            Icon: Users,
            href: mcpDetailTabHref(routes, toolsetSlug, "team-access"),
            active: activeTab === "team-access",
          },
        ]
      : []),
    {
      key: "resources",
      title: "Resources",
      Icon: Database,
      href: mcpDetailTabHref(routes, toolsetSlug, "resources"),
      active: activeTab === "resources",
    },
    {
      key: "prompts",
      title: "Prompts",
      Icon: MessageSquareText,
      href: mcpDetailTabHref(routes, toolsetSlug, "prompts"),
      active: activeTab === "prompts",
    },
    {
      key: "settings",
      title: "Settings",
      Icon: SettingsIcon,
      href: mcpDetailTabHref(routes, toolsetSlug, "settings"),
      active: activeTab === "settings",
    },
  ];

  const cardContent = toolset && (
    <>
      <div className="flex items-center justify-between gap-1">
        <Type className="truncate font-semibold">{toolset.name}</Type>
        <RenameMCPServerButton toolset={toolset} />
      </div>

      <div className="flex flex-col gap-1.5">
        <McpSidebarInfoLabel>Visibility</McpSidebarInfoLabel>
        <MCPStatusDropdown toolset={toolset} />
      </div>

      {mcpUrl && (
        <div className="flex flex-col gap-1">
          <McpSidebarInfoLabel>URL</McpSidebarInfoLabel>
          <div className="flex items-start gap-1">
            <Type
              variant="small"
              muted
              className="line-clamp-2 font-mono text-xs break-all"
            >
              {mcpUrl.replace(/^https?:\/\//, "")}
            </Type>
            <CopyButton
              text={mcpUrl}
              size="inline"
              tooltip="Copy URL"
              className="mt-[-2px] shrink-0"
            />
          </div>
        </div>
      )}

      <div className="flex flex-col gap-1">
        <McpSidebarInfoLabel>Tools</McpSidebarInfoLabel>
        <Type variant="small">{toolset.tools?.length ?? 0}</Type>
      </div>

      <div className="border-border flex items-stretch border-t pt-3">
        <a
          href={installPageUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="text-muted-foreground hover:text-foreground flex flex-1 items-center justify-center gap-1 text-xs font-semibold transition-colors hover:no-underline"
        >
          Installation page
          <ExternalLink className="h-3 w-3" />
        </a>
        <div className="bg-border w-px self-stretch" />
        <routes.playground.Link
          queryParams={{ toolset: toolset.slug }}
          className="flex flex-1 items-center justify-center hover:no-underline"
        >
          <span className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs font-semibold transition-colors">
            Test in Playground
            <ArrowRight className="h-3 w-3" />
          </span>
        </routes.playground.Link>
      </div>
    </>
  );

  return (
    <McpSidebarNavShell
      backHref={routes.mcp.href()}
      cardContent={cardContent}
      items={items}
    />
  );
}
