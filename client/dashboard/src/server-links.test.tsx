import { renderHook } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

// Route templates come out of the real href builders when the slugs are the
// route parameters themselves.
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: ":orgSlug", projectSlug: ":projectSlug" }),
}));

import { PORTABLE_PATH_PREFIX } from "./lib/portable-path";
import { WIZARD_STEP_IDS } from "./pages/agents/credentials/wizard-steps";
import { useOrgRoutes, useRoutes } from "./routes";
import serverLinks from "./server-links.json";
import { STANDALONE_ROUTES } from "./standalone-routes";

type ProjectRoutes = ReturnType<typeof useRoutes>;
type OrgRoutes = ReturnType<typeof useOrgRoutes>;
type LinkName = keyof typeof serverLinks.links;

/**
 * The dashboard path each server link opens, built the way the dashboard
 * builds its own links. A route that moves or is renamed changes (or fails to
 * compile) here, and the comparison with server-links.json fails.
 */
const dashboardPaths = (
  routes: ProjectRoutes,
  org: OrgRoutes,
): Record<LinkName, string> => ({
  Self: PORTABLE_PATH_PREFIX,
  Block: STANDALONE_ROUTES.block,
  SharedSkill: STANDALONE_ROUTES.sharedSkill,
  RiskPolicyChallengeAcknowledge:
    STANDALONE_ROUTES.riskPolicyChallengeAcknowledge,
  RiskPolicyChallengeAcknowledgeShort:
    STANDALONE_ROUTES.riskPolicyChallengeAcknowledgeShort,
  RiskPolicyBypassRequest: STANDALONE_ROUTES.riskPolicyBypassRequest,
  OrganizationHome: org.home.href(),
  Billing: org.billing.href(),
  Domains: org.domains.href(),
  Identity: org.identity.href(),
  DeviceAgent: org.deviceAgent.href(),
  DataExports: org.dataExports.href(),
  EventFeed: org.data.href(),
  AgentManagement: org.legacyAgents.href(),
  AccessRoles: org.access.roles.href(),
  AccessGrant: org.access.roles.href(),
  AccessChallenges: org.access.challenges.href(),
  RequestAccess: org.requestAccess.href(),
  Assistants: routes.assistants.href(),
  NewAssistant: routes.assistants.newAssistant.href(),
  Assistant: routes.assistants.detail.href(":assistantId"),
  AssistantThread: routes.assistants.detail.href(":assistantId"),
  AssistantSessions: routes.assistants.detail.href(":assistantId"),
  Chat: routes.chat.conversation.href(":chatId"),
  Deployment: routes.deployments.deployment.href(":deploymentId"),
  Dashboard: routes.dashboards.detail.href(":dashboardId"),
  SignalsIntelligence: routes.signalsIntelligence.href(),
  Logs: routes.logs.href(),
  Plugins: routes.plugins.href(),
  MCPServer: routes.mcp.details.href(":mcpServerSlug"),
  MCPServerInspect: routes.mcp.details.inspect.href(":mcpServerSlug"),
  MCPServerAuthentication: routes.mcp.details.settings.href(":mcpServerSlug"),
  MCPGateway: routes.mcp.gateway.href(":gatewayId"),
  ShadowAIMCP: routes.shadowAI.mcps.detail.href(":serverSlug"),
  RiskPolicies: routes.policyCenter.href(),
  MCPApprovals: routes.policyCenter.href(),
  ToolApprovalRequest: routes.policyCenter.href(),
  RiskEvents: routes.riskEvents.href(),
});

function renderPaths(): Record<LinkName, string> {
  const { result } = renderHook(
    () => dashboardPaths(useRoutes(), useOrgRoutes()),
    { wrapper: MemoryRouter },
  );
  return result.current;
}

const templates = Object.entries(serverLinks.links) as [LinkName, string][];

describe("server-links.json", () => {
  it("names a dashboard page for every link the server builds", () => {
    expect(Object.keys(renderPaths()).sort()).toEqual(
      Object.keys(serverLinks.links).sort(),
    );
  });

  it.each(templates)("%s opens a dashboard route", (name, template) => {
    const path = template.split(/[?#]/)[0];
    expect(renderPaths()[name]).toBe(path);
  });

  it.each(templates)("%s names only real agent wizard steps", (_, template) => {
    const query = (template.split("#")[0] ?? "").split("?")[1] ?? "";
    for (const step of new URLSearchParams(query).getAll("step")) {
      expect(WIZARD_STEP_IDS).toContain(step);
    }
  });
});
