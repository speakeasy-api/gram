import { DetailBody } from "@/components/detail-body";
import { useMcpServerDetailChrome } from "@/components/mcp/useMcpServerDetailChrome";
import { McpDetailFrame } from "@/components/mcp/McpDetailFrame";
import { useProviderSetupRollout } from "@/hooks/useProviderSetupRollout";
import { ProviderSetupPage } from "./setup/ProviderSetupPage";
import { SetupModal } from "./setup/SetupModal";
import { isKnownEnumValue } from "@/lib/enum";
import { UpdateMcpServerFormVisibility as KnownUpdateMcpServerFormVisibility } from "@gram/client/models/components/updatemcpserverform.js";
import { SplashScreen } from "@/components/ui/LogoLoader";
import { RequireScope } from "@/components/require-scope";
import { AttachedUserSessions } from "@/components/sessions/AttachedUserSessions";
import { useTabScrollReset } from "@/hooks/useTabScrollReset";
import { platformEndpointSlug } from "@/hooks/useToolsetUrl";
import {
  cn,
  firstPartyConnectUrl,
  getServerURL,
  supportsFirstPartyConnect,
} from "@/lib/utils";
import { getMcpServerArgs, mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { isNotNotFound } from "@/lib/query-errors";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Switch } from "@/components/ui/Switch";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { ArrowRight, Check, ChevronDown, Loader2 } from "lucide-react";
import {
  Link,
  Navigate,
  useLocation,
  useNavigate,
  useParams,
} from "react-router";
import { MCPTeamAccessTab } from "../MCPTeamAccessTab";
import { RemoteTeamAccessTab } from "./tabs/RemoteTeamAccessTab";
import {
  activeTabFromPath,
  mcpServerTabHref,
  MCP_SERVER_TAB_URLS,
  standsInForToolset,
  tabForServer,
  tabSegmentFromPath,
} from "./MCPServerDetailsRouting";
import { ToolsetRedirectPage } from "../ToolsetRedirect";
import { serverTabForToolsetTab } from "../toolsetRedirectRouting";
import { isNotFoundError } from "@/lib/route-errors";
import { HostedContentTab } from "./hosted/HostedContentTabs";
import { HostedToolsTab } from "./hosted/HostedToolsTab";
import { useHostedToolset } from "./hosted/useHostedToolset";
import { useHostedVisibilityGuard } from "./hosted/useHostedVisibilityGuard";
import { InspectTab } from "./tabs/InspectTab";
import { OverviewTab } from "./tabs/OverviewTab";
import { MCP_AGENT_SETUP_SECTION_ID } from "./tabs/settings/sections/AgentSetupSection";
import { MCP_PUBLIC_ACCESS_SECTION_ID } from "./tabs/settings/sections/PublicAccessSection";
import { ClientsAndSessionsTab } from "@/components/sessions/ClientsAndSessionsTab";
import { SettingsTab } from "./tabs/settings/SettingsTab";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { GuardrailsTab } from "./tabs/guardrails/GuardrailsTab";
import { RemoteSkillsTabRoute } from "./tabs/skills/RemoteSkillsTab";
import { MCPSkillsTabRoute } from "../MCPSkillsTab";
import {
  ALL_VISIBILITY_OPTIONS,
  HOSTED_PUBLIC_DESCRIPTION,
  useMcpServerVisibilityUpdate,
  visibilityLabel,
  VISIBILITY_OPTIONS,
} from "./mcpServerVisibility";

export default function MCPServerDetails(): JSX.Element | null {
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const location = useLocation();
  const routes = useRoutes();
  const providerSetupRollout = useProviderSetupRollout();
  const navigate = useNavigate();
  const mcpScopedFlag = useFeatureFlag(FEATURE_FLAGS.mcpScopedPolicies);
  const mcpScoped = mcpScopedFlag.status === "enabled";
  const idOrSlug = mcpServerSlug ?? "";
  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const tabContentRef = useTabScrollReset(activeTab);
  const chrome = useMcpServerDetailChrome();
  const tabSegment = tabSegmentFromPath(location.pathname, idOrSlug);

  const {
    data: mcpServer,
    error: mcpServerError,
    isLoading,
    isError,
  } = useGetMcpServer(getMcpServerArgs(idOrSlug), undefined, {
    enabled: idOrSlug !== "",
    throwOnError: isNotNotFound,
  });

  const mcpServerId = mcpServer?.id ?? "";
  const hosted = mcpServer !== undefined && standsInForToolset(mcpServer);
  const {
    toolset: hostedToolset,
    isLoading: isLoadingToolset,
    isError: toolsetFailed,
    refetch: refetchToolset,
  } = useHostedToolset(hosted ? mcpServer : undefined);

  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ mcpServerId }, undefined, {
      enabled: mcpServerId !== "",
    });
  const endpoints = endpointsResult?.mcpEndpoints ?? [];
  const platformSlug = platformEndpointSlug(endpoints);
  const platformConnectUrl = supportsFirstPartyConnect({
    userSessionIssuerId: mcpServer?.userSessionIssuerId,
    tunneledMcpServerId: mcpServer?.tunneledMcpServerId,
    visibility: mcpServer?.visibility,
    platformSlug,
  })
    ? firstPartyConnectUrl(
        platformSlug ? `${getServerURL()}/mcp/${platformSlug}` : undefined,
      )
    : undefined;

  if (!idOrSlug) {
    return <Navigate to={routes.mcp.href()} replace />;
  }
  // Not a server: maybe a legacy toolset slug or `/mcp/x` link.
  if (isNotFoundError(mcpServerError)) {
    return (
      <ToolsetRedirectPage
        toolsetSlug={idOrSlug}
        tabSegment={tabSegment}
        serverLookupMissed
      />
    );
  }
  if (isError || (!isLoading && !mcpServer)) {
    return <Navigate to={routes.mcp.href()} replace />;
  }
  // Which tabs exist depends on the server's kind, so route only once it loads.
  if (!mcpServer) return <SplashScreen />;
  // Setup renders inside the frame like every other tab, but only once the
  // rollout is known: a disabled one sends the viewer to Overview.
  if (activeTab === "setup" && providerSetupRollout === "disabled") {
    return (
      <Navigate
        to={routes.mcp.details.overview.href(mcpServerRouteParam(mcpServer))}
        replace
      />
    );
  }
  // Retired tab segments (`authentication`, the toolset page's `performance`).
  if (tabSegment !== undefined && !activeTab) {
    const target = serverTabForToolsetTab(tabSegment, location.hash);
    return (
      <Navigate
        to={`${mcpServerTabHref(routes, idOrSlug, tabForServer(target.tab, hosted))}${location.search}${target.hash}`}
        replace
      />
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
  if (activeTab && mcpServer && tabForServer(activeTab, hosted) !== activeTab) {
    return (
      <Navigate
        to={`${mcpServerTabHref(routes, idOrSlug, tabForServer(activeTab, hosted))}${location.search}`}
        replace
      />
    );
  }
  if (!activeTab) {
    const target = serverTabForToolsetTab(undefined, location.hash);
    return (
      <Navigate
        to={`${mcpServerTabHref(routes, idOrSlug, tabForServer(target.tab, hosted))}${location.search}${target.hash}`}
        replace
      />
    );
  }
  const renderTabContent = () => {
    // Hosted tabs and settings need the toolset; never fall back to another layout.
    // Overview needs the loaded toolset only when the read's summary is missing.
    // Skills reads links by server id and owns its flag and access checks.
    const needsToolset =
      activeTab === "overview"
        ? !mcpServer.toolset
        : activeTab !== "sessions" &&
          activeTab !== "guardrails" &&
          activeTab !== "skills" &&
          activeTab !== "setup";
    if (hosted && needsToolset && !hostedToolset) {
      if (isLoadingToolset) return <SplashScreen />;
      return (
        <DetailBody spacing="none" className="px-0 pt-0">
          <Alert variant="error" dismissible={false}>
            <Stack gap={2}>
              <Text small>
                {toolsetFailed
                  ? "This server's tools could not be loaded."
                  : "This server's tools are no longer available."}
              </Text>
              <div>
                <Button variant="secondary" size="sm" onClick={refetchToolset}>
                  <Button.Text>Retry</Button.Text>
                </Button>
              </div>
            </Stack>
          </Alert>
        </DetailBody>
      );
    }
    switch (activeTab) {
      case "setup":
        if (providerSetupRollout === "loading") return <SplashScreen />;
        // Setup is a modal over Overview; closing it lands there.
        return (
          <>
            <OverviewTab
              mcpServer={mcpServer}
              hostedToolset={hostedToolset}
              agentSetupHref={`${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AGENT_SETUP_SECTION_ID}`}
            />
            <SetupModal
              onClose={() =>
                void navigate(
                  routes.mcp.details.overview.href(
                    mcpServerRouteParam(mcpServer),
                  ),
                )
              }
            >
              <RequireScope
                scope="mcp:write"
                resourceId={mcpServer.id}
                level="page"
              >
                <ProviderSetupPage key={mcpServer.id} mcpServer={mcpServer} />
              </RequireScope>
            </SetupModal>
          </>
        );
      case "tools":
        return (
          hostedToolset && (
            <DetailBody spacing="none" className="px-0 pt-0">
              <HostedToolsTab toolset={hostedToolset} />
            </DetailBody>
          )
        );
      case "resources":
        return (
          hostedToolset && (
            <DetailBody spacing="none" className="px-0 pt-0">
              <HostedContentTab toolset={hostedToolset} kind="resources" />
            </DetailBody>
          )
        );
      case "prompts":
        return (
          hostedToolset && (
            <DetailBody spacing="none" className="px-0 pt-0">
              <HostedContentTab toolset={hostedToolset} kind="prompts" />
            </DetailBody>
          )
        );
      case "overview":
        return (
          mcpServer && (
            <OverviewTab
              mcpServer={mcpServer}
              hostedToolset={hostedToolset}
              agentSetupHref={`${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AGENT_SETUP_SECTION_ID}`}
            />
          )
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
      case "skills":
        // Hosted servers list their own links; remote-backed servers list
        // theirs with a note on how agents load them. The routes check the
        // flag before the scope, and any other server lands on the overview.
        return (
          mcpServer &&
          (hosted ? (
            <MCPSkillsTabRoute mcpServer={mcpServer} mcpServerSlug={idOrSlug} />
          ) : (
            <RemoteSkillsTabRoute
              mcpServer={mcpServer}
              mcpServerSlug={idOrSlug}
            />
          ))
        );
      case "team-access":
        return (
          mcpServer && (
            <RequireScope scope="org:read" level="page">
              <RequireScope
                scope="mcp:read"
                resourceId={mcpServer.toolsetId ?? mcpServer.id}
                level="page"
              >
                {/* Reads gate on the toolset id like the API; grants key on the server id that endpoint calls check. */}
                {mcpServer.remoteMcpServerId ? (
                  <RemoteTeamAccessTab server={mcpServer} />
                ) : (
                  <MCPTeamAccessTab
                    resourceId={mcpServer.id}
                    serverName={mcpServer.name ?? undefined}
                    tools={hostedToolset?.tools}
                  />
                )}
              </RequireScope>
            </RequireScope>
          )
        );
      case "guardrails":
        // Wait out the flag lookup rather than bouncing a deep link to it.
        if (mcpScopedFlag.status === "loading") return <SplashScreen />;
        return mcpServer && mcpScoped && !mcpServer.unproxiedMcpServerId ? (
          <GuardrailsTab mcpServer={mcpServer} />
        ) : (
          mcpServer && (
            <Navigate
              to={mcpServerTabHref(routes, idOrSlug, "overview")}
              replace
            />
          )
        );
      case "sessions":
        return (
          mcpServer && (
            <RequireScope scope="project:read" level="page">
              <ClientsAndSessionsTab
                issuerId={mcpServer.userSessionIssuerId}
                originatingMcpServerId={mcpServer.id}
                attachedSessions={
                  mcpServer.userSessionIssuerId ? (
                    <AttachedUserSessions
                      issuerId={mcpServer.userSessionIssuerId}
                      connectUrl={platformConnectUrl}
                    />
                  ) : undefined
                }
              />
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
              hostedToolset={hosted ? hostedToolset : undefined}
            />
          )
        );
    }
  };

  return (
    <McpDetailFrame
      chrome={chrome}
      breadcrumbs={{
        substitutions: { [idOrSlug]: mcpServer?.name || "MCP Server" },
        // skipSegments matches by literal value, not position — if the
        // server's own slug happens to collide with a tab name (e.g. a
        // server slugged "settings"), guard against also skipping the
        // server's own breadcrumb crumb.
        skipSegments: MCP_SERVER_TAB_URLS.filter((tab) => tab !== idOrSlug),
      }}
      contentRef={tabContentRef}
    >
      {renderTabContent()}
    </McpDetailFrame>
  );
}

export function MCPServerStatusDropdown({
  server,
}: {
  server: McpServer;
}): JSX.Element {
  const { canWrite, updateVisibility, updating, displayVisibility } =
    useMcpServerVisibilityUpdate(server);
  const hosted = standsInForToolset(server);
  const guard = useHostedVisibilityGuard({
    server,
    hosted,
    apply: updateVisibility,
    isApplying: updating,
  });
  const isTunneled = Boolean(server.tunneledMcpServerId);
  const { data: tunneledSource } = useGetTunneledMcpServer(
    getTunneledMcpServerArgs(server.tunneledMcpServerId ?? ""),
    undefined,
    { enabled: isTunneled },
  );
  const sourceAllowsPublic = tunneledSource?.allowPublic ?? false;
  const options =
    isTunneled || hosted ? ALL_VISIBILITY_OPTIONS : VISIBILITY_OPTIONS;
  // The requested state shows while the change saves, so the control answers
  // at once; a spinner replaces the dot until it lands.
  const currentLabel = visibilityLabel(displayVisibility);
  const currentDotClass =
    ALL_VISIBILITY_OPTIONS.find((option) => option.value === displayVisibility)
      ?.dotClass ?? "bg-muted-foreground";
  // Public is gated on the source's consent; the option stays visible but
  // disabled, and this footer takes the owner to where consent is given.
  const publicBlocked = isTunneled && !sourceAllowsPublic;
  const routes = useRoutes();
  const publicAccessHref = `${mcpServerTabHref(routes, mcpServerRouteParam(server), "settings")}#${MCP_PUBLIC_ACCESS_SECTION_ID}`;

  // Unproxied servers have no Speakeasy-hosted endpoint for disabled/private to
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
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild disabled={!canWrite}>
          <button
            type="button"
            title={
              isKnownEnumValue(
                KnownUpdateMcpServerFormVisibility,
                server.visibility,
              )
                ? undefined
                : "Refresh the page before changing this server’s availability."
            }
            disabled={!canWrite}
            aria-busy={updating}
            className="text-foreground hover:bg-muted trans border-border flex w-fit items-center gap-2 border px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
          >
            {updating ? (
              <Loader2
                aria-label="Saving"
                className="text-muted-foreground size-3 shrink-0 animate-spin"
              />
            ) : (
              <span
                className={cn("h-2 w-2 shrink-0 rounded-full", currentDotClass)}
              />
            )}
            {currentLabel}
            <ChevronDown className="text-muted-foreground h-3 w-3" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-[320px] p-1">
          {options.map((option) => {
            const optionBlocked =
              option.value === "public" &&
              (publicBlocked || guard.publicUnavailable);
            return (
              <DropdownMenuItem
                key={option.value}
                // Visibly unavailable while a change saves.
                disabled={optionBlocked || updating}
                onSelect={() => {
                  if (optionBlocked || updating) return;
                  if (hosted) guard.request(option.value);
                  else updateVisibility(option.value);
                }}
                className="group flex cursor-pointer items-start gap-2.5 p-2 data-[disabled]:cursor-not-allowed data-[disabled]:opacity-60"
              >
                {option.value === displayVisibility ? (
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
                    {optionBlocked
                      ? publicBlocked
                        ? "Enable public access on the tunnel source first to allow anonymous serving."
                        : "Loading environment data…"
                      : hosted && option.value === "public"
                        ? HOSTED_PUBLIC_DESCRIPTION
                        : option.description}
                  </span>
                </div>
              </DropdownMenuItem>
            );
          })}
          {publicBlocked && (
            <DropdownMenuItem asChild className="mt-1 border-t">
              <Link
                to={publicAccessHref}
                className="text-muted-foreground hover:text-foreground flex cursor-pointer items-center gap-1 px-2 py-2 text-xs hover:no-underline"
              >
                Enable public access in Settings
                <ArrowRight className="size-3" />
              </Link>
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      {guard.dialogs}
    </>
  );
}

export function MCPServerAvailabilityToggle({
  server,
}: {
  server: McpServer;
}): JSX.Element {
  const { canWrite, updateVisibility, updating } =
    useMcpServerVisibilityUpdate(server);
  const known = isKnownEnumValue(
    KnownUpdateMcpServerFormVisibility,
    server.visibility,
  );
  const available =
    server.visibility === "private" || server.visibility === "public";

  let label = available ? "Disable MCP server" : "Enable MCP server";
  if (!known) label = "MCP server availability unknown";
  return (
    <span
      title={
        known
          ? undefined
          : "Refresh the page before changing this server’s availability."
      }
    >
      <Switch
        checked={available}
        // Hosted servers change availability through the dropdown's confirmations.
        disabled={!canWrite || updating || standsInForToolset(server)}
        aria-label={label}
        onCheckedChange={(checked) => {
          updateVisibility(checked ? "private" : "disabled");
        }}
      />
    </span>
  );
}
