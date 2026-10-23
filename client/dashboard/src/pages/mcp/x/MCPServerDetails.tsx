import { isKnownEnumValue } from "@/lib/enum";
import { useFollowRenamedServer } from "@/pages/mcp/x/useFollowRenamedServer";
import { UpdateMcpServerFormVisibility as KnownUpdateMcpServerFormVisibility } from "@gram/client/models/components/updatemcpserverform.js";
import {
  invalidateMcpServerQueries,
  mcpServerVisibilityToast,
  mcpServerVisibilityUpdateForm,
} from "@/lib/mcp-server-visibility";
import { handleError } from "@/lib/errors";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { AttachedUserSessions } from "@/components/sessions/AttachedUserSessions";
import { useRBAC } from "@/hooks/useRBAC";
import { useTelemetry } from "@/contexts/Telemetry";
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
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
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
import { useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Check, ChevronDown } from "lucide-react";
import { Link, Navigate, useLocation, useParams } from "react-router";
import { toast } from "sonner";
import { MCPTeamAccessTab } from "../MCPTeamAccessTab";
import {
  activeTabFromPath,
  initialTabFromHash,
  isLegacyAuthenticationTabPath,
  mcpServerTabHref,
  MCP_SERVER_TAB_URLS,
  standsInForToolset,
  tabForServer,
} from "./MCPServerDetailsRouting";
import {
  HostedPromptsTab,
  HostedResourcesTab,
} from "./hosted/HostedContentTabs";
import { HostedToolsTab } from "./hosted/HostedToolsTab";
import { useHostedToolset } from "./hosted/useHostedToolset";
import { useHostedVisibilityGuard } from "./hosted/useHostedVisibilityGuard";
import { InspectTab } from "./tabs/InspectTab";
import { OverviewTab } from "./tabs/OverviewTab";
import { MCP_AGENT_SETUP_SECTION_ID } from "./tabs/settings/sections/AgentSetupSection";
import { MCP_AUTHENTICATION_SECTION_ID } from "./tabs/settings/sections/authentication/AuthenticationSection";
import { MCP_PUBLIC_ACCESS_SECTION_ID } from "./tabs/settings/sections/PublicAccessSection";
import { ClientsAndSessionsTab } from "@/components/sessions/ClientsAndSessionsTab";
import { SettingsTab } from "./tabs/settings/SettingsTab";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { GuardrailsTab } from "./tabs/guardrails/GuardrailsTab";

export default function MCPServerDetails(): JSX.Element | null {
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const location = useLocation();
  const routes = useRoutes();
  const mcpScopedFlag = useFeatureFlag(FEATURE_FLAGS.mcpScopedPolicies);
  const mcpScoped = mcpScopedFlag.status === "enabled";
  const idOrSlug = mcpServerSlug ?? "";
  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const tabContentRef = useTabScrollReset(activeTab);
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
  if (isError || (!isLoading && !mcpServer)) {
    return <Navigate to={routes.mcp.href()} replace />;
  }
  // Which tabs exist depends on the server's kind, so route only once it loads.
  if (!mcpServer) return null;
  if (legacyAuthenticationPath) {
    return (
      <Navigate
        to={`${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AUTHENTICATION_SECTION_ID}`}
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
    const initialTab = tabForServer(initialTabFromHash(location.hash), hosted);
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
    // Hosted tabs and settings need the toolset; never fall back to another layout.
    // Overview needs the loaded toolset only when the read's summary is missing.
    const needsToolset =
      activeTab === "overview"
        ? !mcpServer.toolset
        : activeTab !== "sessions" && activeTab !== "guardrails";
    if (hosted && needsToolset && !hostedToolset) {
      if (isLoadingToolset) return null;
      return (
        <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
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
        </div>
      );
    }
    switch (activeTab) {
      case "tools":
        return (
          hostedToolset && (
            <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
              <HostedToolsTab toolset={hostedToolset} />
            </div>
          )
        );
      case "resources":
        return (
          hostedToolset && (
            <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
              <HostedResourcesTab toolset={hostedToolset} />
            </div>
          )
        );
      case "prompts":
        return (
          hostedToolset && (
            <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
              <HostedPromptsTab toolset={hostedToolset} />
            </div>
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
                <MCPTeamAccessTab
                  resourceId={mcpServer.id}
                  serverName={mcpServer.name ?? undefined}
                  tools={hostedToolset?.tools}
                />
              </RequireScope>
            </RequireScope>
          )
        );
      case "guardrails":
        // Wait out the flag lookup rather than bouncing a deep link to it.
        if (mcpScopedFlag.status === "loading") return null;
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

// Public visibility is offered for hosted servers, and for tunneled-backed
// servers once the tunnel source owner has consented (double opt-in).
const PUBLIC_VISIBILITY_OPTION: VisibilityOption = {
  value: "public",
  label: "Public",
  description:
    "Anyone can connect anonymously — no login. Every tool is exposed to the public internet.",
  dotClass: "bg-green-400",
  hoverDotClass: "group-hover:bg-green-400",
};

const HOSTED_PUBLIC_DESCRIPTION =
  "Anyone with the URL can read the tools hosted by this server. Authentication is still required to use the tools.";

const ALL_VISIBILITY_OPTIONS = [
  ...VISIBILITY_OPTIONS,
  PUBLIC_VISIBILITY_OPTION,
];

export function MCPServerStatusDropdown({
  server,
}: {
  server: McpServer;
}): JSX.Element {
  const { canWrite, updateVisibility, updating } =
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
  const currentLabel = visibilityLabel(server.visibility);
  const currentDotClass =
    ALL_VISIBILITY_OPTIONS.find((option) => option.value === server.visibility)
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
        <DropdownMenuTrigger asChild disabled={!canWrite || updating}>
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
            disabled={!canWrite || updating}
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
            const optionBlocked =
              option.value === "public" &&
              (publicBlocked || guard.publicUnavailable);
            return (
              <DropdownMenuItem
                key={option.value}
                disabled={optionBlocked}
                onSelect={() => {
                  if (optionBlocked) return;
                  if (hosted) guard.request(option.value);
                  else updateVisibility(option.value);
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

function visibilityLabel(visibility: McpServerVisibility): string {
  switch (visibility) {
    case "disabled":
      return "Disabled";
    case "public":
      return "Public";
    case "private":
      return "Private";
  }
  return "Unknown";
}

function useMcpServerVisibilityUpdate(server: McpServer): {
  canWrite: boolean;
  updateVisibility: (visibility: McpServerVisibility) => void;
  updating: boolean;
} {
  const { hasScope } = useRBAC();
  const canWrite =
    hasScope("mcp:write") &&
    isKnownEnumValue(KnownUpdateMcpServerFormVisibility, server.visibility);
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const followRenamedServer = useFollowRenamedServer();
  const update = useUpdateMcpServerMutation({
    onSuccess: async (updated, variables) => {
      followRenamedServer(updated);
      await invalidateMcpServerQueries(queryClient);
      const next = variables.request.updateMcpServerForm.visibility;
      toast.success(mcpServerVisibilityToast(next));
      // Keeps the toolset page's visibility events for hosted servers.
      if (standsInForToolset(server)) {
        telemetry.capture("mcp_event", {
          action:
            next === "disabled"
              ? "mcp_disabled"
              : next === "public"
                ? "mcp_made_public"
                : "mcp_made_private",
          slug: server.toolset?.slug,
        });
      }
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update server visibility",
      );
    },
  });

  const updateVisibility = (next: McpServerVisibility) => {
    if (!canWrite || next === server.visibility) return;
    try {
      update.mutate({
        request: {
          updateMcpServerForm: mcpServerVisibilityUpdateForm(server, next),
        },
      });
    } catch (error) {
      handleError(error, { title: "Couldn't update server availability" });
    }
  };

  return { canWrite, updateVisibility, updating: update.isPending };
}
