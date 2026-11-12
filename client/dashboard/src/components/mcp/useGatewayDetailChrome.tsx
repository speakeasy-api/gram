import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useSkillsOverMcpEnabled } from "@/pages/skills/use-skills-over-mcp";
import {
  gatewayInstallPageUrl,
  useResolvedMcpServerUrl,
} from "@/hooks/useToolsetUrl";
import {
  activeTabFromPath,
  gatewayTabHref,
  type TabValue,
} from "@/pages/mcp/gateway/GatewayDetailsRouting";
import { useGatewayMemberRows } from "@/pages/mcp/gateway/useGatewayMemberRows";
import {
  type GatewayMemberStates,
  useGatewayMemberStates,
} from "@/pages/mcp/gateway/useGatewayMemberStates";
import type { MemberRow } from "@/pages/mcp/gateway/memberRows";
import { SETTINGS_GROUPS } from "@/components/detail/settings-groups";
import { mcpServerTabHref } from "@/pages/mcp/x/MCPServerDetailsRouting";
import {
  type GatewayMemberSetupGap,
  useGatewayMemberSetup,
} from "./useGatewayMemberSetup";
import { GATEWAY_AUTHENTICATION_SECTION_ID } from "@/pages/mcp/gateway/GatewaySettingsTab";
import {
  GATEWAY_ADD_SERVERS_HASH,
  GATEWAY_SERVERS_SECTION_ID,
} from "@/pages/mcp/gateway/GatewayMembersSection";
import { MCP_SERVER_URL_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/ServerUrlSection";
import { useRoutes } from "@/routes";
import { useGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { Network } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  MCP_ENVIRONMENT_VARIABLES_SECTION_ID,
  type McpDetailChrome,
  type McpReadinessCheck,
} from "./mcpDetailChrome";
import { buildMcpDetailNavItems } from "./mcpDetailNavItems";
import { useLocation, useParams } from "react-router";

/**
 * Loads one gateway's detail chrome: its tabs, readiness checks and URLs, in
 * the same shape as an MCP server's so both pages share one header.
 */
export function useGatewayDetailChrome(): McpDetailChrome | null {
  const routes = useRoutes();
  const location = useLocation();
  const { gatewayId } = useParams<{ gatewayId: string }>();

  const id = gatewayId ?? "";
  const { data: metaMcpServer } = useGetMetaMcpServer({ id }, undefined, {
    enabled: id !== "",
  });
  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ metaMcpServerId: id }, undefined, {
      enabled: id !== "",
    });
  const endpoints = endpointsResult?.mcpEndpoints ?? [];
  const { mcpUrl } = useResolvedMcpServerUrl(endpoints, isLoadingEndpoints);
  const installPageUrl = gatewayInstallPageUrl(endpoints);
  const {
    rows,
    isLoading: isLoadingMembers,
    isError: membersFailed,
  } = useGatewayMemberRows(id);
  const memberSetup = useGatewayMemberSetup(rows);
  // One sign-in status call; tool checks only if Overview already ran them.
  const memberStates = useGatewayMemberStates(
    metaMcpServer,
    endpoints,
    isLoadingEndpoints,
    rows,
    { checkTools: "cache" },
  );
  const { hasScope } = useRBAC();
  // Mirrors the mcp_servers sidebar: the tab reads org membership as well as
  // the resource, so hide it rather than render a permission wall.
  const canViewTeamAccess =
    !!metaMcpServer && hasScope("org:read") && hasScope("mcp:read", id);
  const project = useProject();
  // Same check as the Skills tab route: skill read on this project.
  const canViewSkills =
    useSkillsOverMcpEnabled() && hasScope("skill:read", project.id, project.id);
  if (!id || !metaMcpServer) return null;

  const activeTab = activeTabFromPath(location.pathname, id);
  // Same rule as the Overview's Members tile: unproxied and slugless members
  // sit in the list but the gateway cannot dispatch to them.
  const servable = rows.filter(
    (row) =>
      row.classification === "hosted" || row.classification === "proxied",
  ).length;

  // A member's own settings, linked by id: two servers can share a slug.
  const memberSettingsHref = (serverId: string, anchor: string) =>
    `${mcpServerTabHref(routes, serverId, "settings")}#${anchor}`;
  const overviewHref = gatewayTabHref(routes, id, "overview");

  const readinessChecks: McpReadinessCheck[] = metaMcpServer
    ? [
        {
          key: "server-url",
          label: "Gateway URL",
          description: isLoadingEndpoints
            ? "Checking this gateway's URL."
            : mcpUrl
              ? "Endpoint is live and ready to connect to."
              : "Add an endpoint so this gateway has a URL to connect to.",
          ready: isLoadingEndpoints ? null : !!mcpUrl,
          pending: isLoadingEndpoints,
          href: `${gatewayTabHref(routes, id, "settings")}#${MCP_SERVER_URL_SECTION_ID}`,
        },
        {
          key: "members",
          label: "Members",
          description: isLoadingMembers
            ? "Checking this gateway's members."
            : membersFailed
              ? "This gateway's members could not be loaded."
              : servable > 0
                ? `Fronting ${servable} MCP ${servable === 1 ? "server" : "servers"}.`
                : rows.length > 0
                  ? "None of this gateway's members can be served. Add a hosted or proxied MCP server."
                  : "Add MCP servers for this gateway to front.",
          ready: isLoadingMembers || membersFailed ? null : servable > 0,
          pending: isLoadingMembers,
          chipLabel: rows.length > 0 ? "No servable members" : "No members",
          // Member management lives on the Overview tab: an empty gateway
          // opens the Add servers sheet, otherwise the list is shown.
          href:
            rows.length > 0
              ? `${overviewHref}#${GATEWAY_SERVERS_SECTION_ID}`
              : `${overviewHref}#${GATEWAY_ADD_SERVERS_HASH}`,
        },
        // Only hosted members carry environment variables of their own.
        ...(rows.some((row) => row.classification === "hosted")
          ? [
              memberSetupCheck(memberSetup, isLoadingMembers, (gap) =>
                memberSettingsHref(
                  gap.serverId,
                  MCP_ENVIRONMENT_VARIABLES_SECTION_ID,
                ),
              ),
            ]
          : []),
        ...memberAccessChecks(
          rows,
          memberStates,
          gatewayTabHref(routes, id, "inspect"),
          (serverId) =>
            memberSettingsHref(serverId, SETTINGS_GROUPS.identity.id),
          `${gatewayTabHref(routes, id, "settings")}#${GATEWAY_AUTHENTICATION_SECTION_ID}`,
        ),
        // Only a gateway set to Code Mode can be let down by it.
        ...(metaMcpServer.discoveryMode === "code_mode"
          ? [
              {
                key: "code-mode",
                label: "Code Mode",
                description: metaMcpServer.codeModeEnabled
                  ? "Agents write code against this gateway's tools."
                  : "Code Mode isn't available for this gateway, so agents get Progressive instead. Choose another mode in Settings.",
                ready: !!metaMcpServer.codeModeEnabled,
                broken: true,
                href: `${gatewayTabHref(routes, id, "settings")}#${SETTINGS_GROUPS.tools.id}`,
              },
            ]
          : []),
        {
          key: "authentication",
          label: "Authentication",
          description: metaMcpServer.userSessionIssuerId
            ? "User sessions are configured for this gateway."
            : "Anyone who knows the URL can connect. Add an issuer to require sign-in.",
          ready: !!metaMcpServer.userSessionIssuerId,
          // Anonymous gateways are supported, so sign-in is a choice, not a
          // step left.
          optional: true,
          href: `${gatewayTabHref(routes, id, "settings")}#${GATEWAY_AUTHENTICATION_SECTION_ID}`,
        },
      ]
    : [];

  const items = buildMcpDetailNavItems<TabValue>({
    tabs: [
      "overview",
      "inspect",
      canViewSkills && "skills",
      canViewTeamAccess && "team-access",
      "sessions",
      "settings",
    ],
    activeTab,
    href: (tab) => gatewayTabHref(routes, id, tab),
  });

  return {
    name: metaMcpServer.name,
    icon: (className) => (
      <Network className={cn("text-muted-foreground", className)} />
    ),
    kindLabel: "Gateway",
    statusControl: null,
    primaryUrl: mcpUrl,
    readinessChecks,
    installLinks: installPageUrl
      ? [{ url: installPageUrl, label: "Install page" }]
      : [],
    items,
  };
}

/**
 * Whether every hosted member can run its tools: a member missing required
 * environment variables fails each call the gateway sends it.
 */
function memberSetupCheck(
  setup: ReturnType<typeof useGatewayMemberSetup>,
  loadingMembers: boolean,
  settingsHref: (gap: GatewayMemberSetupGap) => string,
): McpReadinessCheck {
  const item = { key: "member-setup", label: "Member setup", broken: true };
  const [first] = setup.gaps;
  if (first) {
    const described = setup.gaps.map(
      (gap) =>
        `${gap.name} needs ${gap.missingEnvVars} environment ${gap.missingEnvVars === 1 ? "variable" : "variables"} set before its tools can run.`,
    );
    return {
      ...item,
      description: described.join(" "),
      ready: false,
      href: settingsHref(first),
    };
  }
  if (loadingMembers || setup.loading) {
    return {
      ...item,
      description: "Checking that each member is set up.",
      ready: null,
      pending: true,
    };
  }
  if (setup.failed) {
    return {
      ...item,
      description: "Could not check every member's setup.",
      ready: null,
    };
  }
  return {
    ...item,
    description: "Every hosted member has what it needs to run its tools.",
    ready: true,
  };
}

/**
 * What the viewer still has to do, or can't use, among the servable members:
 * a sign-in step per member that needs the viewer's account, and a broken
 * check per member whose tools couldn't be read. Healthy members add nothing;
 * while any member is still being checked, one pending check holds the
 * header back from reading Ready.
 */
function memberAccessChecks(
  rows: MemberRow[],
  states: GatewayMemberStates,
  inspectHref: string,
  signInSettingsHref: (serverId: string) => string,
  gatewaySignInHref: string,
): McpReadinessCheck[] {
  const checks: McpReadinessCheck[] = [];
  let checking = false;
  let checkFailed = false;
  for (const row of rows) {
    const state = states.stateFor(row);
    if (!state) continue;
    const slug = row.server?.slug ?? row.member.mcpServerSlug ?? row.member.id;
    const name = row.server?.name ?? row.member.mcpServerSlug ?? "A member";
    if (state.kind === "checking") checking = true;
    if (state.kind === "sign_in") {
      checks.push({
        key: `member-sign-in-${slug}`,
        label: `Sign in to ${name}`,
        description: `Connect your own account so its tools work for you. Inspect has the Connect step.`,
        ready: false,
        href: inspectHref,
      });
    }
    // A failed check says nothing about the member, so it is no issue.
    if (state.kind === "unavailable" && state.checkFailed) {
      checkFailed = true;
      continue;
    }
    if (state.kind === "unavailable" && state.needsGatewaySignIn) {
      checks.push({
        key: `member-unavailable-${slug}`,
        label: `Set up sign-in for ${name}`,
        description: `${name}'s sign-in isn't set up on this gateway. Add its authorization server here.`,
        ready: false,
        broken: true,
        href: gatewaySignInHref,
      });
      continue;
    }
    if (state.kind === "unavailable") {
      // The server's own words go in the tooltip, never the message.
      const description = state.needsCredential
        ? `${name} needs a shared credential before the gateway can list its tools.`
        : state.fromServer
          ? `The gateway couldn't list ${name}'s tools. Inspect shows what went wrong.`
          : state.why;
      checks.push({
        key: `member-unavailable-${slug}`,
        label: `${name}'s tools couldn't be read`,
        description,
        detail: state.fromServer ? state.why : undefined,
        ready: false,
        broken: true,
        href:
          state.needsCredential && row.server
            ? signInSettingsHref(row.server.id)
            : inspectHref,
      });
    }
  }
  if (checkFailed && !checking) {
    checks.push({
      key: "member-access",
      label: "Member access",
      description:
        "Couldn't check what each member gives you. Inspect can try again.",
      ready: null,
      href: inspectHref,
    });
  }
  if (checking) {
    checks.push({
      key: "member-access",
      label: "Member access",
      description: "Checking what each member gives you.",
      ready: null,
      pending: true,
    });
  }
  return checks;
}
