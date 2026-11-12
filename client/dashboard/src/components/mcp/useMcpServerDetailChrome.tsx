import { PluginStatusBanner } from "@/pages/mcp/overview/PluginStatusBanner";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  pluginServerRef,
  serverMatchesRef,
} from "@/pages/mcp/overview/serverMatchesRef";
import type { HostedServerRef } from "@/pages/mcp/overview/MCPOverviewTab";
import type { McpDetailChrome } from "./mcpDetailChrome";
import { useProviderSetupRollout } from "@/hooks/useProviderSetupRollout";
import { setupProviderForUrl } from "@/pages/mcp/x/setup/provider-setup";
import { useOrganization, useProject } from "@/contexts/Auth";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { useSkillsOverMcpEnabled } from "@/pages/skills/use-skills-over-mcp";
import { type ReadinessCheck } from "@/components/mcp-server-readiness-bar";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { SetupGuideCard } from "@/components/setup-guide/SetupGuideCard";
import { getMcpServerArgs } from "@/lib/sources";
import {
  mcpServerInstallPageLinks,
  usePrivateMcpServerUrls,
} from "@/hooks/usePrivateMcpServerUrls";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { useRBAC } from "@/hooks/useRBAC";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import {
  activeTabFromPath,
  isCanonicalHostedWrapper,
  mcpServerTabHref,
  standsInForToolset,
  type TabValue,
} from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useHostedToolset } from "@/pages/mcp/x/hosted/useHostedToolset";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { MCP_AGENT_SETUP_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/AgentSetupSection";
import { MCP_AUTHENTICATION_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection";
import {
  tunnelAgentReadiness,
  useTunneledMcpConnections,
} from "@/pages/mcp/x/tabs/tunnelStatus";
import { MCP_SERVER_URL_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/ServerUrlSection";
import { MCP_VISIBILITY_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/VisibilitySection";
import { useRoutes } from "@/routes";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { isNotNotFound } from "@/lib/query-errors";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useGetUnproxiedMcpServer } from "@gram/client/react-query/getUnproxiedMcpServer.js";
import { McpServerNetworkAccessMode } from "@gram/client/models/components/mcpserver.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { usePlugins } from "@gram/client/react-query/plugins";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import {
  AlertTriangle,
  Database,
  MessageSquareText,
  ShieldCheck,
  Wrench,
} from "lucide-react";
import { useMcpServerAuthenticationReadiness } from "@/hooks/useMcpServerAuthenticationReadiness";
import { useLocation, useParams } from "react-router";

import {
  McpServerCardStatus,
  RemoteIdentitySummary,
  UpstreamCredentialsIdentitySummary,
} from "./McpDetailRows";
import {
  buildMcpDetailNavItems,
  type McpDetailTabDef,
} from "./mcpDetailNavItems";

const SETUP_TAB: McpDetailTabDef<TabValue> = {
  key: "setup",
  title: "Setup",
  Icon: Wrench,
};
const HOSTED_TABS: McpDetailTabDef<TabValue>[] = [
  { key: "tools", title: "Tools", Icon: Wrench },
  { key: "resources", title: "Resources", Icon: Database },
  { key: "prompts", title: "Prompts", Icon: MessageSquareText },
];
const GUARDRAILS_TAB: McpDetailTabDef<TabValue> = {
  key: "guardrails",
  title: "Guardrails",
  Icon: ShieldCheck,
};

/**
 * Loads one MCP server's detail chrome: its tabs (with the per-kind and
 * per-scope rules), readiness checks, status control, and URLs.
 */
export function useMcpServerDetailChrome(): McpDetailChrome | null {
  const providerSetupRollout = useProviderSetupRollout();
  const routes = useRoutes();
  const location = useLocation();
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const { hasScope, hasAnyScope } = useRBAC();
  const mcpScoped =
    useFeatureFlag(FEATURE_FLAGS.mcpScopedPolicies).status === "enabled";
  const organization = useOrganization();
  const project = useProject();
  const skillsOverMcp = useSkillsOverMcpEnabled();
  const pluginScope = usePluginQueryScope();
  const canWritePlugins = usePluginWriteAccess();
  const canReadPlugins =
    canWritePlugins || hasAnyScope(["org:read", "org:admin"], organization.id);

  const idOrSlug = mcpServerSlug ?? "";
  const { data: mcpServer } = useGetMcpServer(
    getMcpServerArgs(idOrSlug),
    undefined,
    { enabled: idOrSlug !== "", throwOnError: isNotNotFound },
  );
  const mcpServerId = mcpServer?.id ?? "";
  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ mcpServerId }, undefined, {
      enabled: mcpServerId !== "",
    });
  const endpoints = endpointsResult?.mcpEndpoints ?? [];
  const { mcpUrl, installPageUrl } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );
  const {
    privateMcpUrls,
    privateInstallPageUrls,
    canReadPrivateUrls,
    isLoading: isLoadingPrivateUrls,
    isError: privateUrlsError,
  } = usePrivateMcpServerUrls(mcpServer, endpoints);
  const publicRoutesEnabled =
    mcpServer?.networkAccessMode !== McpServerNetworkAccessMode.PrivateOnly;
  const effectiveInstallPageLinks = mcpServerInstallPageLinks(
    mcpServer?.networkAccessMode,
    installPageUrl,
    privateInstallPageUrls,
  );
  const privateUrlAssessable =
    canReadPrivateUrls && !isLoadingPrivateUrls && !privateUrlsError;
  const serverUrlReady =
    mcpServer?.networkAccessMode === McpServerNetworkAccessMode.PrivateOnly
      ? privateMcpUrls.length > 0
      : !!mcpUrl;

  const remoteMcpServerId = mcpServer?.remoteMcpServerId ?? "";
  const { data: remoteMcpServer } = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "", throwOnError: false },
  );
  const unproxiedMcpServerId = mcpServer?.unproxiedMcpServerId ?? "";
  const { data: unproxiedMcpServer } = useGetUnproxiedMcpServer(
    { id: unproxiedMcpServerId },
    undefined,
    { enabled: unproxiedMcpServerId !== "" },
  );
  const upstreamUrl = remoteMcpServer?.url ?? unproxiedMcpServer?.url;
  // Reads the snapshot the Overview's tunnel status polls, so the readiness
  // card and the status beside it agree without a second poller.
  const tunnelConnections = useTunneledMcpConnections(
    mcpServer?.tunneledMcpServerId ?? "",
  );

  const authReadiness = useMcpServerAuthenticationReadiness(mcpServer);
  const {
    remoteIdentityMode,
    passThroughAuthorization,
    identityUnavailable,
    isLoadingRemoteIdentity,
    upstreamAuth,
    upstreamAuthQuery,
    identityProbeStatus,
  } = authReadiness;

  // Mirrors PluginStatusBanner's isTrulyPublished: server membership in a
  // plugin alone isn't "included" if the marketplace repo was never
  // published, since a teammate can't actually install it yet.
  const { data: pluginsData } = usePlugins(pluginScope, undefined, {
    enabled: canReadPlugins,
  });
  const { data: publishStatus } = usePublishStatus(pluginScope, undefined, {
    enabled: canReadPlugins,
  });
  const isCanonical = !!mcpServer && isCanonicalHostedWrapper(mcpServer);
  // The same load the detail page does, so it is shared from the cache; it
  // finds the toolset from the list when the read omits its summary.
  const {
    toolset: canonicalToolset,
    isError: canonicalToolsetFailed,
    isLoading: isLoadingCanonicalToolset,
  } = useHostedToolset(isCanonical ? mcpServer : undefined);
  const canonicalToolsetSlug = canonicalToolset?.slug;
  const { data: environmentsData, isPending: isPendingEnvironments } =
    useListEnvironments(undefined, undefined, {
      enabled: canonicalToolsetSlug !== undefined,
    });
  const { data: canonicalMetadata, isPending: isPendingMetadata } =
    useGetMcpMetadata({ toolsetSlug: canonicalToolsetSlug ?? "" }, undefined, {
      enabled: canonicalToolsetSlug !== undefined,
      throwOnError: false,
    });

  const pluginRef: HostedServerRef | undefined = mcpServer
    ? pluginServerRef(mcpServer, canonicalToolset)
    : undefined;
  // Same membership rule as the header's plugin card.
  const memberPlugins = pluginRef
    ? (pluginsData?.plugins ?? []).filter((plugin) =>
        plugin.servers?.some((s) => serverMatchesRef(s, pluginRef)),
      )
    : [];
  const isPluginMember = memberPlugins.length > 0;
  // A marketplace with no active collaborator isn't installable either.
  const marketplaceReady = !!(
    publishStatus?.repoOwner &&
    publishStatus.repoName &&
    publishStatus.hasCollaborators !== false
  );
  const isTrulyIncluded = isPluginMember && marketplaceReady;
  // Settings carries the warning the toolset page put on Authentication.
  const missingRequiredEnvVars = useMissingRequiredEnvVars(
    canonicalToolset,
    environmentsData?.environments ?? [],
    canonicalToolset?.defaultEnvironmentSlug || "default",
    canonicalMetadata?.metadata,
  );

  if (!idOrSlug) return null;

  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const isRemoteBacked = !!mcpServer?.remoteMcpServerId;
  const isTunneledBacked = !!mcpServer?.tunneledMcpServerId;
  const isUnproxied = !!mcpServer?.unproxiedMcpServerId;
  const isSourceBacked = isRemoteBacked || isTunneledBacked || isUnproxied;
  const hosted = !!mcpServer && standsInForToolset(mcpServer);
  // A hosted server is granted under its toolset id, like the API checks it.
  const grantResourceId = mcpServer?.toolsetId ?? mcpServer?.id ?? "";
  const canViewTeamAccess =
    !!mcpServer &&
    hasScope("org:read", organization.id) &&
    hasScope("mcp:read", grantResourceId);
  // Same check as the Skills tab routes: skill read on this project. Only
  // hosted and remote-backed servers have the tab.
  const canViewSkills =
    skillsOverMcp &&
    (hosted || isRemoteBacked) &&
    hasScope("skill:read", project.id, project.id);

  // Guardrails police traffic that passes through Speakeasy, so unproxied servers
  // (which never do) have none. Admin-only, like the policies it manages.
  const canViewGuardrails =
    mcpScoped &&
    !!mcpServer &&
    !isUnproxied &&
    hasScope("org:admin", organization.id) &&
    hasScope("mcp:read", mcpServer.id);

  // Source detail pages are gone; the description still explains the backend,
  // but there is no separate page to send anyone to.
  let sourceDescription = "Connect an MCP server as this server's source.";
  if (hosted) {
    sourceDescription = "Hosted by Speakeasy.";
  } else if (mcpServer?.remoteMcpServerId) {
    sourceDescription = "Backed by a remote MCP server.";
  } else if (mcpServer?.tunneledMcpServerId) {
    sourceDescription = "Backed by a tunneled MCP server.";
  } else if (mcpServer?.unproxiedMcpServerId) {
    sourceDescription = "Backed by an unproxied MCP server.";
  }

  const readinessChecks: ReadinessCheck[] = mcpServer
    ? [
        // Unproxied servers have no switch: clients reach the vendor directly.
        ...(isUnproxied
          ? []
          : [
              {
                key: "availability",
                label: "Availability",
                description:
                  mcpServer.visibility === "disabled"
                    ? "This server is turned off, so every connection to it fails. Turn it on in Settings."
                    : "Turned on and accepting connections.",
                ready: mcpServer.visibility !== "disabled",
                broken: true,
                href: `${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_VISIBILITY_SECTION_ID}`,
              },
            ]),
        {
          key: "server-url",
          label: "Server URL",
          description: isUnproxied
            ? "Not applicable — unproxied servers have no Speakeasy-hosted endpoint."
            : mcpServer.networkAccessMode ===
                  McpServerNetworkAccessMode.PrivateOnly &&
                !privateUrlAssessable
              ? !canReadPrivateUrls
                ? "Private URL visibility requires organization admin access."
                : isLoadingPrivateUrls
                  ? "Checking private URL availability."
                  : "Private URL availability could not be checked."
              : serverUrlReady
                ? "Endpoint is live and ready to connect to."
                : mcpServer.networkAccessMode ===
                    McpServerNetworkAccessMode.PrivateOnly
                  ? "Bring private ingress online so this server has a URL to connect to."
                  : "Add an endpoint so this server has a URL to connect to.",
          ready: isUnproxied || serverUrlReady,
          href: isUnproxied
            ? undefined
            : `${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_SERVER_URL_SECTION_ID}`,
        },
        {
          key: "authentication",
          label: "Authentication",
          ...(hosted
            ? hostedAuthenticationCheck({
                canonical: isCanonical,
                toolsetLoaded: canonicalToolset !== undefined,
                toolsetLoading: isLoadingCanonicalToolset,
                toolsetFailed: canonicalToolsetFailed,
                settling: isPendingEnvironments || isPendingMetadata,
                missingRequiredEnvVars,
              })
            : {
                description: authReadiness.description,
                ready: authReadiness.ready,
              }),
          href: `${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AUTHENTICATION_SECTION_ID}`,
        },
        {
          key: "source",
          label: "Source",
          description: sourceDescription,
          // A hosted server's tools are its own; it has no source to connect.
          ready: hosted || isSourceBacked,
        },
        ...(isTunneledBacked
          ? [
              tunnelAgentReadiness({
                connections: tunnelConnections.data,
                loading: tunnelConnections.isLoading,
                error: tunnelConnections.isError,
                agentSetupHref: `${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AGENT_SETUP_SECTION_ID}`,
                overviewHref: mcpServerTabHref(routes, idOrSlug, "overview"),
              }),
            ]
          : []),
        ...(canReadPlugins
          ? [
              {
                key: "plugin",
                label: "Included in a plugin",
                description: isTrulyIncluded
                  ? `Published to ${memberPlugins.length} plugin${memberPlugins.length > 1 ? "s" : ""}.`
                  : isPluginMember
                    ? "Marketplace needs publishing before this plugin is installable."
                    : "Add this server to a plugin so your team can install it.",
                ready: isTrulyIncluded,
                // Distribution is a later choice, not part of making the
                // server work, so it never counts as a step left.
                optional: true,
                href: routes.plugins.href(),
              },
            ]
          : []),
      ]
    : [];

  const showSetup =
    providerSetupRollout === "enabled" &&
    !!mcpServer?.remoteMcpServerId &&
    !!setupProviderForUrl(upstreamUrl) &&
    hasScope("mcp:write", mcpServer.id);
  const items = buildMcpDetailNavItems<TabValue>({
    tabs: [
      showSetup && SETUP_TAB,
      "overview",
      // Inspect sits second on every kind of server. Hidden for unproxied
      // servers: there's no reliable way to list their tools yet (the vendor's
      // own auth blocks an anonymous probe, and there's no fallback source).
      !isUnproxied && "inspect",
      ...(hosted ? HOSTED_TABS : []),
      canViewSkills && "skills",
      canViewTeamAccess && "team-access",
      canViewGuardrails && GUARDRAILS_TAB,
      // Hidden for unproxied servers: the customer connects straight to the
      // vendor with the vendor's own credentials, so Speakeasy never mints a session
      // or registers a client for them and the tab would always be empty.
      !isUnproxied && "sessions",
      "settings",
    ],
    activeTab,
    href: (tab) => mcpServerTabHref(routes, idOrSlug, tab),
    titleNodes: {
      settings:
        missingRequiredEnvVars > 0 ? (
          <span className="flex items-center gap-1.5">
            Settings
            <SimpleTooltip
              tooltip={`${missingRequiredEnvVars === 1 ? "1 required environment variable is" : `${missingRequiredEnvVars} required environment variables are`} not set, so tool calls that need ${missingRequiredEnvVars === 1 ? "it" : "them"} will fail. Set ${missingRequiredEnvVars === 1 ? "it" : "them"} under Sign-in and identity.`}
            >
              <AlertTriangle
                className="text-warning h-3.5 w-3.5 shrink-0"
                aria-label="Required environment variables not set"
              />
            </SimpleTooltip>
          </span>
        ) : undefined,
    },
  });

  const identity =
    mcpServer && isRemoteBacked ? (
      upstreamAuth !== "legacy" ? (
        <UpstreamCredentialsIdentitySummary
          status={upstreamAuthQuery.data?.status}
          authenticationRequired={
            identityProbeStatus === "authentication-required"
          }
          unavailable={upstreamAuth === "error" || upstreamAuthQuery.isError}
          loading={upstreamAuth === "loading" || upstreamAuthQuery.isPending}
          settingsHref={`${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AUTHENTICATION_SECTION_ID}`}
        />
      ) : (
        <RemoteIdentitySummary
          mode={remoteIdentityMode}
          passThroughAuthorization={!!passThroughAuthorization}
          authenticationRequired={
            identityProbeStatus === "authentication-required"
          }
          unavailable={identityUnavailable}
          loading={isLoadingRemoteIdentity}
          settingsHref={`${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AUTHENTICATION_SECTION_ID}`}
        />
      )
    ) : undefined;

  if (!mcpServer) return null;
  const kindLabel = isRemoteBacked
    ? "Remote MCP"
    : isTunneledBacked
      ? "Tunneled MCP"
      : isUnproxied
        ? "Unproxied MCP"
        : hosted
          ? "Hosted MCP"
          : undefined;

  return {
    name: mcpServer.name || "MCP Server",
    icon: (className) => (
      <SourceMcpIcon mcpServerId={mcpServer.id} className={className} />
    ),
    kindLabel,
    statusControl: <McpServerCardStatus server={mcpServer} />,
    primaryUrl: publicRoutesEnabled ? mcpUrl : privateMcpUrls[0],
    readinessChecks,
    // The upstream endpoint is what the guide catalog indexes; a server
    // with no upstream has no guide to point at.
    setupGuide: upstreamUrl ? (
      <SetupGuideCard serverUrl={upstreamUrl} />
    ) : undefined,
    identity,
    installLinks: effectiveInstallPageLinks,
    aside: pluginRef ? (
      <PluginStatusBanner variant="header" server={pluginRef} />
    ) : undefined,
    items,
  };
}

/**
 * A hosted server's Authentication step. Speakeasy signs users in itself, so
 * the step left is the toolset's required environment variables, which the
 * Authentication settings section asks for.
 */
function hostedAuthenticationCheck({
  canonical,
  toolsetLoaded,
  toolsetLoading,
  toolsetFailed,
  settling,
  missingRequiredEnvVars,
}: {
  /** Only the toolset's own wrapper has its variables checked here. */
  canonical: boolean;
  toolsetLoaded: boolean;
  /** The lookup is in flight; a finished lookup with no toolset is a miss. */
  toolsetLoading: boolean;
  toolsetFailed: boolean;
  settling: boolean;
  missingRequiredEnvVars: number;
}): Pick<ReadinessCheck, "description" | "ready"> {
  if (!canonical) {
    return { description: "Speakeasy handles sign-in.", ready: true };
  }
  if (toolsetFailed) {
    return {
      description: "Required environment variables could not be checked.",
      ready: null,
    };
  }
  if (!toolsetLoaded && !toolsetLoading) {
    return {
      description:
        "This server's toolset could not be found, so its required environment variables cannot be checked.",
      ready: null,
    };
  }
  if (!toolsetLoaded || settling) {
    return {
      description: "Checking required environment variables.",
      ready: null,
    };
  }
  if (missingRequiredEnvVars > 0) {
    return {
      description: `Set ${missingRequiredEnvVars} required environment variable${missingRequiredEnvVars > 1 ? "s" : ""} so the tools can reach their API.`,
      ready: false,
    };
  }
  return {
    description:
      "Speakeasy handles sign-in, and every required variable is set.",
    ready: true,
  };
}
