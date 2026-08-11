import {
  DetailSidebarInfoLabel,
  DetailSidebarNav,
  type DetailSidebarNavItem,
} from "@/components/detail/detail-sidebar-nav";
import { useExternalMcpOAuthConfigStatus } from "@/components/sources/sources-hooks";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useToolset } from "@/hooks/toolTypes";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { useRBAC } from "@/hooks/useRBAC";
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
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import {
  AlertTriangle,
  ArrowRight,
  Database,
  ExternalLink,
  Gauge,
  KeyRound,
  LayoutDashboard,
  MessageSquareText,
  Plug,
  Settings as SettingsIcon,
  Users,
  Wrench,
} from "lucide-react";
import * as React from "react";
import { useLocation, useParams } from "react-router";

export function McpDetailSidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
  const location = useLocation();
  const { toolsetSlug } = useParams<{ toolsetSlug: string }>();

  const { data: toolset } = useToolset(toolsetSlug);
  const { hasScope } = useRBAC();
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
  const canViewTeamAccess =
    !!toolset && hasScope("org:read") && hasScope("mcp:read", toolset.id);

  const items: DetailSidebarNavItem[] = [
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
    ...(canViewTeamAccess
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
      key: "sessions",
      title: "Clients and Sessions",
      Icon: Plug,
      href: mcpDetailTabHref(routes, toolsetSlug, "sessions"),
      active: activeTab === "sessions",
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
        <Text className="truncate font-semibold">{toolset.name}</Text>
        <RenameMCPServerButton toolset={toolset} />
      </div>

      <div className="flex flex-col gap-1.5">
        <DetailSidebarInfoLabel>Visibility</DetailSidebarInfoLabel>
        <MCPStatusDropdown toolset={toolset} />
      </div>

      {mcpUrl && (
        <div className="flex flex-col gap-1">
          <DetailSidebarInfoLabel>URL</DetailSidebarInfoLabel>
          <div className="flex items-start gap-1">
            <Text
              variant="small"
              muted
              className="line-clamp-2 font-mono text-xs break-all"
            >
              {mcpUrl.replace(/^https?:\/\//, "")}
            </Text>
            <CopyButton
              text={mcpUrl}
              size="xs"
              tooltip="Copy URL"
              className="mt-[-2px] shrink-0"
            />
          </div>
        </div>
      )}

      <div className="flex flex-col gap-1">
        <DetailSidebarInfoLabel>Tools</DetailSidebarInfoLabel>
        <Text variant="small">{toolset.tools?.length ?? 0}</Text>
      </div>

      <div className="border-border flex items-stretch border-t pt-3">
        {installPageUrl && (
          <>
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
          </>
        )}
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
    <DetailSidebarNav
      backHref={routes.mcp.href()}
      cardContent={cardContent}
      items={items}
    />
  );
}
