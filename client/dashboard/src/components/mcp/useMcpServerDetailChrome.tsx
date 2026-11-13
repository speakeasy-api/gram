import { PluginStatusBanner } from "@/pages/mcp/overview/PluginStatusBanner";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  pluginServerRef,
  serverMatchesRef,
} from "@/pages/mcp/overview/serverMatchesRef";
import type { HostedServerRef } from "@/pages/mcp/overview/MCPOverviewTab";
import {
  MCP_ENVIRONMENT_VARIABLES_SECTION_ID,
  type McpDetailChrome,
  type McpReadinessCheck,
} from "./mcpDetailChrome";
import { SETTINGS_GROUPS } from "@/components/detail/settings-groups";
import { UNKNOWN_STATUS, UPSTREAM_AUTH_STATUS } from "@/lib/auth-clients";
import { enumPresentation } from "@/lib/enum";
import type { IdentityMode } from "@/lib/remote-identity";
import {
  type McpServerReachability,
  useMcpServerReachability,
} from "./useMcpServerReachability";
import { useProviderSetupRollout } from "@/hooks/useProviderSetupRollout";
import {
  type SetupProvider,
  setupProviderForUrl,
} from "@/pages/mcp/x/setup/provider-setup";
import { useOrganization, useProject } from "@/contexts/Auth";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { useSkillsOverMcpEnabled } from "@/pages/skills/use-skills-over-mcp";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { useSetupGuide } from "@/components/setup-guide/useSetupGuide";
import { formatRelativeTime } from "@/lib/dates";
import { getMcpServerArgs } from "@/lib/sources";
import {
  mcpServerInstallPageLinks,
  usePrivateMcpServerUrls,
} from "@/hooks/usePrivateMcpServerUrls";
import {
  useCustomDomains,
  useMcpEndpointAddresses,
  useResolvedMcpServerUrl,
} from "@/hooks/useToolsetUrl";
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
import {
  type McpServerAuthenticationReadiness,
  useMcpServerAuthenticationReadiness,
} from "@/hooks/useMcpServerAuthenticationReadiness";
import { useLocation, useParams } from "react-router";

import { McpServerHeaderStatus } from "./McpDetailRows";
import { SetupPrompt } from "./SetupPrompt";
import {
  buildMcpDetailNavItems,
  type McpDetailTabDef,
} from "./mcpDetailNavItems";

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
  const {
    mcpUrl,
    installPageUrl,
    loading: isResolvingUrl,
  } = useResolvedMcpServerUrl(endpoints, isLoadingEndpoints);
  // A custom-domain endpoint has no URL until its domain resolves; until
  // then the server isn't missing one, it is still being checked.
  const hasCustomDomainEndpoint = endpoints.some(
    (endpoint) => endpoint.customDomainId,
  );
  const { isLoading: isLoadingCustomDomains } = useCustomDomains(
    hasCustomDomainEndpoint,
  );
  const resolvingCustomDomain =
    hasCustomDomainEndpoint && !mcpUrl && isLoadingCustomDomains;
  const {
    privateMcpUrls,
    privateInstallPageUrls,
    canReadPrivateUrls,
    isLoading: isLoadingPrivateUrls,
    isError: privateUrlsError,
  } = usePrivateMcpServerUrls(mcpServer, endpoints);
  const publicRoutesEnabled =
    mcpServer?.networkAccessMode !== McpServerNetworkAccessMode.PrivateOnly;
  const publicAddresses = useMcpEndpointAddresses(endpoints);
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
  // The listing Inspect makes, shared from the cache: an endpoint only counts
  // as live once the server behind it answers.
  const reachability = useMcpServerReachability({
    mcpUrl,
    mcpServerId: mcpServer?.id,
    projectId: mcpServer?.projectId,
    userSessionIssuerId: mcpServer?.userSessionIssuerId,
    // Private-only servers aren't reached on the hosted URL, which may be
    // turned off on purpose, so probing it would report a working server down.
    enabled:
      !!mcpServer?.remoteMcpServerId &&
      mcpServer.visibility !== "disabled" &&
      mcpServer.networkAccessMode !== McpServerNetworkAccessMode.PrivateOnly &&
      !!mcpUrl,
  });

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
  // Manual steps some providers need on their side, e.g. registering an app.
  const setupGuide = useSetupGuide({ serverUrl: upstreamUrl });

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

  const settingsHref = mcpServerTabHref(routes, idOrSlug, "settings");
  const isDisabled = mcpServer?.visibility === "disabled";
  const tunnelCheck = isTunneledBacked
    ? tunnelAgentReadiness({
        connections: tunnelConnections.data,
        loading: tunnelConnections.isLoading,
        error: tunnelConnections.isError,
        agentSetupHref: `${settingsHref}#${MCP_AGENT_SETUP_SECTION_ID}`,
        overviewHref: mcpServerTabHref(routes, idOrSlug, "overview"),
      })
    : undefined;

  // Fixed order, and each check says when it is still loading, so the chip
  // reads the same however the queries race.
  const setupProvider = setupProviderForUrl(upstreamUrl);
  const showSetup =
    providerSetupRollout === "enabled" &&
    !!mcpServer?.remoteMcpServerId &&
    (!!setupProvider || !!setupGuide) &&
    hasScope("mcp:write", mcpServer.id);
  // A provider with a guided setup is set up there, not in Settings: until
  // Speakeasy can sign in to it, the Authentication step and Overview send
  // the admin to the Setup tab.
  const needsSetup =
    showSetup &&
    !!setupProvider &&
    !authReadiness.loading &&
    !authReadiness.ready;
  const setupHref = mcpServerTabHref(routes, idOrSlug, "setup");
  const setupName = setupProvider ? SETUP_PROVIDER_NAME[setupProvider] : "";
  const checks: McpReadinessCheck[] = mcpServer
    ? [
        // Unproxied servers have no switch: clients reach the vendor directly.
        ...(isUnproxied
          ? []
          : [
              {
                key: "availability",
                label: "Availability",
                description: isDisabled
                  ? "This server is turned off, so every connection to it fails. Turn it on in Settings."
                  : "Turned on and accepting connections.",
                ready: !isDisabled,
                broken: true,
                chipLabel: "Off",
                href: `${settingsHref}#${MCP_VISIBILITY_SECTION_ID}`,
              },
            ]),
        serverUrlCheck({
          unproxied: isUnproxied,
          disabled: isDisabled,
          tunnelAgentDown: tunnelCheck?.broken === true,
          privateOnly:
            mcpServer.networkAccessMode ===
            McpServerNetworkAccessMode.PrivateOnly,
          canReadPrivateUrls,
          privateUrlAssessable,
          loadingUrl:
            isLoadingEndpoints ||
            isResolvingUrl ||
            (publicRoutesEnabled && resolvingCustomDomain) ||
            (canReadPrivateUrls && isLoadingPrivateUrls),
          serverUrlReady,
          reachability,
          settingsHref,
        }),
        {
          key: "authentication",
          label: "Authentication",
          // A step may point somewhere more exact, so it comes after.
          href: `${settingsHref}#${MCP_AUTHENTICATION_SECTION_ID}`,
          ...(hosted
            ? hostedAuthenticationCheck({
                canonical: isCanonical,
                toolsetLoaded: canonicalToolset !== undefined,
                toolsetLoading: isLoadingCanonicalToolset,
                toolsetFailed: canonicalToolsetFailed,
                settling: isPendingEnvironments || isPendingMetadata,
                missingRequiredEnvVars,
                envVarsHref: `${settingsHref}#${MCP_ENVIRONMENT_VARIABLES_SECTION_ID}`,
              })
            : isTunneledBacked
              ? tunneledAuthenticationCheck(!!mcpServer.userSessionIssuerId)
              : sourceAuthenticationCheck(authReadiness)),
        },
        {
          key: "source",
          label: "Source",
          description: sourceDescription,
          // A hosted server's tools are its own; it has no source to connect.
          ready: hosted || isSourceBacked,
        },
        ...(tunnelCheck ? [tunnelCheck] : []),
        // Inspect's drift notice, from the listing it already made. Stored
        // hints are a record to keep current, not something broken.
        ...(reachability.kind === "reachable" && reachability.driftCount > 0
          ? [
              {
                key: "tool-drift",
                label: "Tool list out of date",
                description: `${reachability.driftCount} ${reachability.driftCount === 1 ? "tool has" : "tools have"} changed on the server since Speakeasy saved its tool list. Review and sync them in Inspect.`,
                ready: false,
                optional: true,
                href: mcpServerTabHref(routes, idOrSlug, "inspect"),
              },
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

  // A provider with only a written guide is set up on the Setup tab too,
  // after Authentication: most guides need the redirect URL or credentials
  // it sets up. The step stays open until someone marks the guide done.
  const setupGuideDoneAt = mcpServer?.setupGuideCompletedAt;
  const guideName = setupGuide?.only?.title ?? "the provider";
  const setupGuideCheck: McpReadinessCheck | undefined =
    mcpServer && showSetup && !setupProvider
      ? {
          key: "setup-guide",
          label: `Set up ${guideName}`,
          description: setupGuideDoneAt
            ? `Marked done ${formatRelativeTime(setupGuideDoneAt)}.`
            : `A few steps happen in ${guideName}, not in Speakeasy. Follow the setup guide, then mark it done.`,
          ready: !!setupGuideDoneAt,
          chipLabel: `Set up ${guideName}`,
          href: setupHref,
        }
      : undefined;
  const withGuide = setupGuideCheck
    ? checks.flatMap((check) =>
        check.key === "authentication" ? [check, setupGuideCheck] : [check],
      )
    : checks;
  // Overview offers the guide once Speakeasy can sign in, so it never asks
  // for the provider's steps before the step they depend on.
  const authenticationReady =
    checks.find((check) => check.key === "authentication")?.ready === true;
  const overviewSetupGuide =
    setupGuideCheck && !setupGuideDoneAt && authenticationReady ? (
      <SetupPrompt
        name={guideName}
        href={setupHref}
        description={`A few steps happen in ${guideName}, not in Speakeasy. The setup guide walks you through them.`}
      />
    ) : undefined;

  const readinessChecks = needsSetup
    ? withGuide.map((check) =>
        check.key === "authentication" && check.ready === false
          ? {
              ...check,
              description: `Finish ${setupName} setup so Speakeasy can sign in to it for your team.`,
              chipLabel: "Finish setup",
              href: setupHref,
            }
          : check,
      )
    : withGuide;
  const items = buildMcpDetailNavItems<TabValue>({
    tabs: [
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
    statusControl: <McpServerHeaderStatus server={mcpServer} />,
    // With no public URL yet, a ready private ingress stands in.
    primaryUrl: (publicRoutesEnabled ? mcpUrl : undefined) ?? privateMcpUrls[0],
    // The primary URL first, then the rest: private ingress beside a public
    // route, or a Speakeasy URL beside a custom domain.
    addresses: orderAddresses(
      (publicRoutesEnabled ? mcpUrl : undefined) ?? privateMcpUrls[0],
      [
        ...(publicRoutesEnabled ? publicAddresses : []),
        ...privateMcpUrls.map((url) => ({ url, label: "Private network" })),
      ],
    ),
    identity: identitySummary(
      isRemoteBacked || isTunneledBacked,
      authReadiness,
      `${settingsHref}#${SETTINGS_GROUPS.identity.id}`,
    ),
    readinessChecks,
    setupHref: showSetup ? setupHref : undefined,
    // The upstream endpoint is what the guide catalog indexes; a server
    // with no upstream has no guide to point at.
    installLinks: effectiveInstallPageLinks,
    aside: pluginRef ? (
      <PluginStatusBanner variant="header" server={pluginRef} />
    ) : undefined,
    overviewSetup: needsSetup ? (
      <SetupPrompt name={setupName} href={setupHref} />
    ) : (
      overviewSetupGuide
    ),
    overviewBanner: pluginRef ? (
      <PluginStatusBanner variant="banner" server={pluginRef} />
    ) : undefined,
    items,
  };
}

const SETUP_PROVIDER_NAME: Record<SetupProvider, string> = {
  slack: "Slack",
  github: "GitHub",
  salesforce: "Salesforce",
};

// The identity modes' names, as Settings' identity cards call them.
const IDENTITY_LABEL: Record<IdentityMode, string> = {
  user: "User Identity",
  agent: "Shared credential",
  none: "Manual",
};

/**
 * Whose account the server acts as, in the words Settings uses. Under
 * enforced upstream auth that is the credentials status, as in Settings'
 * credentials section; otherwise the identity mode. Nothing until settled.
 */
function identitySummary(
  signsInUpstream: boolean,
  auth: McpServerAuthenticationReadiness,
  href: string,
): McpDetailChrome["identity"] {
  // The header always shows this row, so every state has words: one that
  // can't be settled yet says so rather than dropping the row.
  if (!signsInUpstream) return { label: "Not used", href };
  if (auth.loading || auth.isLoadingRemoteIdentity) {
    return { label: "Checking…", href };
  }
  if (auth.unavailable || auth.identityUnavailable) {
    return { label: "Couldn't check", href, warn: true };
  }
  if (auth.upstreamAuth === "enforced") {
    const status = auth.upstreamAuthQuery.data?.status;
    if (status === undefined) return { label: "Checking…", href };
    const presentation = enumPresentation(
      UPSTREAM_AUTH_STATUS,
      status,
      UNKNOWN_STATUS,
    );
    return {
      label: presentation.label,
      href,
      warn: presentation.tone === "warning",
    };
  }
  return {
    label: auth.passThroughAuthorization
      ? "Needs cleanup"
      : IDENTITY_LABEL[auth.remoteIdentityMode],
    href,
    warn: !!auth.passThroughAuthorization,
  };
}

/** The primary URL first, each address once; one address needs no list. */
function orderAddresses(
  primary: string | undefined,
  all: { url: string; label: string }[],
): { url: string; label: string }[] | undefined {
  const unique = all.filter(
    (address, index) =>
      all.findIndex((other) => other.url === address.url) === index,
  );
  if (unique.length < 2) return undefined;
  return [
    ...unique.filter((address) => address.url === primary),
    ...unique.filter((address) => address.url !== primary),
  ];
}

type CheckState = Pick<
  McpReadinessCheck,
  "description" | "ready" | "pending" | "broken" | "optional" | "chipLabel"
> & { href?: string; detail?: string };

/**
 * The Server URL step. An endpoint only reads as live when the server could
 * use it: not while the server is off or its tunnel agent is gone, and for a
 * remote server not until the server behind it has answered.
 */
function serverUrlCheck({
  unproxied,
  disabled,
  tunnelAgentDown,
  privateOnly,
  canReadPrivateUrls,
  privateUrlAssessable,
  loadingUrl,
  serverUrlReady,
  reachability,
  settingsHref,
}: {
  unproxied: boolean;
  disabled: boolean;
  tunnelAgentDown: boolean;
  privateOnly: boolean;
  canReadPrivateUrls: boolean;
  privateUrlAssessable: boolean;
  loadingUrl: boolean;
  serverUrlReady: boolean;
  reachability: McpServerReachability;
  settingsHref: string;
}): McpReadinessCheck {
  const item = { key: "server-url", label: "Server URL" };
  const urlHref = `${settingsHref}#${MCP_SERVER_URL_SECTION_ID}`;
  const state = ((): CheckState => {
    if (unproxied) {
      return {
        description:
          "Not applicable — unproxied servers have no Speakeasy-hosted endpoint.",
        ready: true,
      };
    }
    // The Availability and Tunnel agent checks name the fix; this one waits.
    if (disabled) {
      return {
        description: "Unavailable while the server is off.",
        ready: null,
      };
    }
    if (tunnelAgentDown) {
      return { description: "Waiting for tunnel agent.", ready: null };
    }
    if (loadingUrl) {
      return {
        description: "Checking this server's URL.",
        ready: null,
        pending: true,
      };
    }
    if (privateOnly && !privateUrlAssessable) {
      return {
        description: !canReadPrivateUrls
          ? "Private URL visibility requires organization admin access."
          : "Private URL availability could not be checked.",
        ready: null,
      };
    }
    if (!serverUrlReady) {
      return {
        description: privateOnly
          ? "Bring private ingress online so this server has a URL to connect to."
          : "Add an endpoint so this server has a URL to connect to.",
        ready: false,
        href: urlHref,
      };
    }
    switch (reachability.kind) {
      case "checking":
        return {
          description: "Checking that the server answers.",
          ready: null,
          pending: true,
        };
      case "unknown":
        return {
          description: "Couldn't check that the server answers.",
          ready: null,
        };
      case "unreachable":
        return {
          description:
            "Speakeasy can't reach the server behind this URL, so every connection fails. Check its upstream URL in Settings.",
          ready: false,
          broken: true,
          chipLabel: "Can't reach server",
          detail: reachability.detail,
          // The upstream URL is edited in General.
          href: `${settingsHref}#${SETTINGS_GROUPS.general.id}`,
        };
      case "reachable":
      case "off":
        return {
          description: "Endpoint is live and ready to connect to.",
          ready: true,
        };
    }
  })();
  return { ...item, href: urlHref, ...state };
}

/**
 * A remote server's Authentication step, held back until its identity
 * queries settle. A server that asks for sign-in with nothing to sign in
 * with fails every call, so that is an issue, not a step.
 */
function sourceAuthenticationCheck(
  auth: McpServerAuthenticationReadiness,
): CheckState {
  if (auth.loading) {
    return {
      description: "Checking how Speakeasy signs in to this server.",
      ready: null,
      pending: true,
    };
  }
  if (auth.unavailable) {
    return {
      description: "Sign-in setup could not be checked.",
      ready: null,
    };
  }
  if (!auth.ready && auth.identityProbeStatus === "authentication-required") {
    return {
      description:
        "The server asks for sign-in, but no identity is set, so every call fails. Choose how Speakeasy signs in under Settings.",
      ready: false,
      broken: true,
      chipLabel: "Needs sign-in",
    };
  }
  return { description: auth.description, ready: auth.ready };
}

/**
 * A tunneled server's Authentication step. Speakeasy sign-in is a choice:
 * without it anyone who knows the URL can connect.
 */
function tunneledAuthenticationCheck(hasIssuer: boolean): CheckState {
  return hasIssuer
    ? {
        description:
          "Speakeasy sign-in is on. Upstream authorization servers are optional.",
        ready: true,
      }
    : {
        description:
          "Anyone who knows the URL can connect. Turn on sign-in in Settings to require it.",
        ready: false,
        optional: true,
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
  envVarsHref,
}: {
  /** Only the toolset's own wrapper has its variables checked here. */
  canonical: boolean;
  toolsetLoaded: boolean;
  /** The lookup is in flight; a finished lookup with no toolset is a miss. */
  toolsetLoading: boolean;
  toolsetFailed: boolean;
  settling: boolean;
  missingRequiredEnvVars: number;
  /** Where the toolset's environment variables are set. */
  envVarsHref: string;
}): CheckState {
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
      pending: true,
    };
  }
  if (missingRequiredEnvVars > 0) {
    return {
      description: `Set ${missingRequiredEnvVars} required environment variable${missingRequiredEnvVars > 1 ? "s" : ""} so the tools can reach their API.`,
      ready: false,
      href: envVarsHref,
    };
  }
  return {
    description:
      "Speakeasy handles sign-in, and every required variable is set.",
    ready: true,
  };
}
