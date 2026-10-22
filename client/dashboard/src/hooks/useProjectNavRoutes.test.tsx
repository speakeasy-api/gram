import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AppRoute } from "@/routes";
import { useProjectNavRoutes } from "./useProjectNavRoutes";

const testState = vi.hoisted(() => ({
  projectId: "project_a",
  orgMemoryEnabled: false,
  productFeatures: {
    isSuccess: true,
    data: { signalsIntelligenceEnabled: false },
  },
}));

function route(title: string, url: string): AppRoute {
  return {
    Icon: () => null,
    Link: ({ children }) => <>{children}</>,
    active: false,
    goTo: () => undefined,
    href: () => `/${url}`,
    title,
    url,
  };
}

const routes = {
  mcpSessions: route("MCP Sessions", "mcp-sessions"),
  remoteIdentityProviders: route(
    "Authorization Servers",
    "remote-identity-providers",
  ),
  agents: route("Agent Identity", "agent-management"),
  agentSessions: route("Agent Sessions", "agent-sessions"),
  assistants: route("Assistants", "assistants"),
  catalog: route("Catalog", "catalog"),
  chat: route("Project Assistant", "chat"),
  skills: route("Skills", "skills"),
  costs: route("Costs", "costs"),
  explore: route("Explore", "explore"),
  dashboards: route("Dashboards", "dashboards"),
  deployments: route("Deployments", "deployments"),
  detectionRules: route("Detection Rules", "detection-rules"),
  identities: {
    ...route("Identities", "identities"),
    people: route("People", "identities/people"),
    agents: route("Agents", "identities/agents"),
  },
  environments: route("Environments", "environments"),
  home: route("Home", ""),
  insights: route("Insights", "insights"),
  logs: route("Logs", "logs"),
  signalsIntelligence: route("Signals intelligence", "signals-intelligence"),
  mcp: route("MCP", "mcp"),
  orgMemory: route("Org Memory", "org-memory"),
  playground: route("Playground", "playground"),
  plugins: route("Plugins", "plugins"),
  policyCenter: route("Guardrails", "risk-policies"),
  riskEvents: route("Risk Events", "risk-events"),
  watchdog: route("Watchdog", "watchdog"),
  settings: route("Project settings", "settings"),
  shadowAI: route("Shadow AI", "shadow-ai"),
  shadowMCP: route("Shadow MCP", "shadow-mcp"),
  sources: route("Sources", "sources"),
};

vi.mock("@/routes", async () => {
  return {
    useRoutes: () => routes,
  };
});

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: testState.projectId }),
  useOrganization: () => ({ id: "organization_a" }),
}));

vi.mock("./useOrgMemoryDeveloperToggle", () => ({
  useOrgMemoryDeveloperToggle: () => [testState.orgMemoryEnabled, vi.fn()],
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => testState.productFeatures,
}));

beforeEach(() => {
  testState.projectId = "project_a";
  testState.orgMemoryEnabled = false;
  testState.productFeatures = {
    isSuccess: true,
    data: { signalsIntelligenceEnabled: false },
  };
});

describe("useProjectNavRoutes", () => {
  it("removes Signals intelligence when entitlement is disabled or a refresh fails", () => {
    testState.productFeatures.data.signalsIntelligenceEnabled = true;
    const { result, rerender } = renderHook(() => useProjectNavRoutes());
    const visible = () =>
      result.current.some(
        (entry) => entry.route === routes.signalsIntelligence,
      );
    expect(visible()).toBe(true);

    // Cached enabled data must not keep navigation open after a failed check.
    testState.productFeatures.isSuccess = false;
    rerender();
    expect(visible()).toBe(false);

    testState.productFeatures.isSuccess = true;
    testState.productFeatures.data.signalsIntelligenceEnabled = false;
    rerender();
    expect(visible()).toBe(false);
  });

  it("includes agents, sessions and remote providers in project navigation", () => {
    const { result } = renderHook(() => useProjectNavRoutes());
    expect(result.current.map((entry) => entry.route)).toEqual(
      expect.arrayContaining([
        routes.identities.agents,
        routes.mcpSessions,
        routes.remoteIdentityProviders,
      ]),
    );
  });

  it("lists the two rosters and keeps the old agents page out of nav", () => {
    const { result } = renderHook(() => useProjectNavRoutes());

    expect(
      result.current.find((entry) => entry.route === routes.agents),
    ).toBeUndefined();
    expect(
      result.current.find((entry) => entry.route === routes.identities.people)
        ?.scope,
    ).toEqual(["project:read"]);
    expect(
      result.current.find((entry) => entry.route === routes.identities.agents),
    ).toBeTruthy();
  });

  it("uses the selected project's read grant for MCP Sessions", () => {
    const { result, rerender } = renderHook(() => useProjectNavRoutes());
    const sessions = () =>
      result.current.find((entry) => entry.route === routes.mcpSessions);
    expect(sessions()?.scope).toEqual(["project:read"]);
    expect(sessions()?.resourceId).toBe("project_a");
    testState.projectId = "project_b";
    rerender();
    expect(sessions()?.resourceId).toBe("project_b");
  });

  it("lists Identity before MCP Gateway, Security and Policy, and Observability", () => {
    const { result } = renderHook(() => useProjectNavRoutes());
    const navRoutes = result.current.map((entry) => entry.route);
    expect(navRoutes.slice(2, 6)).toEqual([
      routes.identities.people,
      routes.identities.agents,
      routes.mcpSessions,
      routes.remoteIdentityProviders,
    ]);
    expect(navRoutes.indexOf(routes.playground)).toBeLessThan(
      navRoutes.indexOf(routes.riskEvents),
    );
    expect(navRoutes.indexOf(routes.policyCenter)).toBeLessThan(
      navRoutes.indexOf(routes.costs),
    );
  });

  it("leaves Shadow AI and Shadow MCP out of nav; they live in Guardrails", () => {
    const { result } = renderHook(() => useProjectNavRoutes());

    const titles = result.current.map((entry) => entry.route.title);
    expect(titles).not.toContain("Shadow AI");
    expect(titles).not.toContain("Shadow MCP");
    expect(titles).toContain("Guardrails");
  });

  it("leaves Approval Requests out of nav", () => {
    const { result } = renderHook(() => useProjectNavRoutes());

    const navTitles = result.current.map((entry) => entry.route.title);

    expect(navTitles).not.toContain("Approval Requests");
  });

  it("uses project-scoped skill read for Skills", () => {
    const { result } = renderHook(() => useProjectNavRoutes());
    const skills = result.current.find(
      (entry) => entry.route === routes.skills,
    );

    expect(skills?.scope).toEqual(["skill:read"]);
    expect(skills?.resourceId).toBe("project_a");
  });

  it("only includes Org Memory when its session toggle is enabled", () => {
    testState.orgMemoryEnabled = false;
    const { result, rerender } = renderHook(() => useProjectNavRoutes());

    expect(
      result.current.some((entry) => entry.route === routes.orgMemory),
    ).toBe(false);

    testState.orgMemoryEnabled = true;
    rerender();

    expect(
      result.current.some((entry) => entry.route === routes.orgMemory),
    ).toBe(true);
  });

  it("lists Explore and Dashboards for project readers", () => {
    const { result } = renderHook(() => useProjectNavRoutes());
    const scopeOf = (target: AppRoute) =>
      result.current.find((entry) => entry.route === target)?.scope;

    expect(scopeOf(routes.explore)).toEqual(["project:read"]);
    expect(scopeOf(routes.dashboards)).toEqual(["project:read"]);
  });

  it("requires organization admin for Watchdog navigation, matching its page", () => {
    const { result } = renderHook(() => useProjectNavRoutes());
    const watchdog = result.current.find(
      (entry) => entry.route === routes.watchdog,
    );

    expect(watchdog?.scope).toEqual(["org:admin"]);
  });

  it("uses assistant read on the active project for assistant surfaces", () => {
    const { result } = renderHook(() => useProjectNavRoutes());
    const assistantRoutes = result.current.filter(
      (entry) =>
        entry.route === routes.chat || entry.route === routes.assistants,
    );

    expect(assistantRoutes).toHaveLength(2);
    expect(assistantRoutes).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          route: routes.chat,
          scope: ["assistant:read"],
          projectId: "project_a",
        }),
        expect.objectContaining({
          route: routes.assistants,
          scope: ["assistant:read"],
          projectId: "project_a",
        }),
      ]),
    );
  });
});
