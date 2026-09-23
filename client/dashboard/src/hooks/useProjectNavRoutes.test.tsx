import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { AppRoute } from "@/routes";
import { useProjectNavRoutes } from "./useProjectNavRoutes";

const testState = vi.hoisted(() => ({
  projectId: "project_a",
  orgMemoryEnabled: false,
  featureFlags: {} as Record<string, FeatureFlagResult>,
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
    "Remote Identity Providers",
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
  deployments: route("Deployments", "deployments"),
  detectionRules: route("Detection Rules", "detection-rules"),
  identities: route("Identities", "identities"),
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
  riskOverview: route("Risk Overview", "risk"),
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

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: (flag: string) => testState.featureFlags[flag],
}));

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

function unavailableFeatureFlag(
  status: "loading" | "missing" | "error",
): FeatureFlagResult {
  return { status };
}

beforeEach(() => {
  testState.projectId = "project_a";
  testState.orgMemoryEnabled = false;
  testState.productFeatures = {
    isSuccess: true,
    data: { signalsIntelligenceEnabled: false },
  };
  testState.featureFlags = {
    [FEATURE_FLAGS.agentManagement]: { status: "enabled" },
    [FEATURE_FLAGS.userSessionsDashboard]: { status: "enabled" },
    [FEATURE_FLAGS.assistants]: unavailableFeatureFlag("loading"),
    [FEATURE_FLAGS.deploymentsPage]: unavailableFeatureFlag("loading"),
    [FEATURE_FLAGS.riskWatchdog]: unavailableFeatureFlag("loading"),
    [FEATURE_FLAGS.explore]: unavailableFeatureFlag("loading"),
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

  it.each(["loading", "disabled", "missing", "error"] as const)(
    "hides agent management when its rollout is %s",
    (status) => {
      testState.featureFlags[FEATURE_FLAGS.agentManagement] = { status };
      const { result } = renderHook(() => useProjectNavRoutes());
      expect(
        result.current.some((entry) => entry.route === routes.agents),
      ).toBe(false);
    },
  );

  it("includes sessions and remote providers in project navigation", () => {
    const { result } = renderHook(() => useProjectNavRoutes());
    expect(result.current.map((entry) => entry.route)).toEqual(
      expect.arrayContaining([
        routes.mcpSessions,
        routes.remoteIdentityProviders,
      ]),
    );
  });

  it("includes Agent Identity for owners without requiring role grants", () => {
    const { result } = renderHook(() => useProjectNavRoutes());
    const agents = result.current.find(
      (entry) => entry.route === routes.agents,
    );

    expect(agents?.scope).toEqual([]);
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
      routes.identities,
      routes.agents,
      routes.mcpSessions,
      routes.remoteIdentityProviders,
    ]);
    expect(navRoutes.indexOf(routes.playground)).toBeLessThan(
      navRoutes.indexOf(routes.riskOverview),
    );
    expect(navRoutes.indexOf(routes.shadowAI)).toBeLessThan(
      navRoutes.indexOf(routes.costs),
    );
  });

  it("uses Shadow AI as the nav destination, with Shadow MCP folded into it", () => {
    const { result } = renderHook(() => useProjectNavRoutes());

    const titles = result.current.map((entry) => entry.route.title);
    expect(titles).toContain("Shadow AI");
    expect(titles).not.toContain("Shadow MCP");
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

  it.each(["loading", "missing", "error"] as const)(
    "preserves opt-in and opt-out navigation while flags are %s",
    (status) => {
      testState.featureFlags = {
        [FEATURE_FLAGS.agentManagement]: { status: "enabled" },
        [FEATURE_FLAGS.userSessionsDashboard]: { status: "enabled" },
        [FEATURE_FLAGS.assistants]: unavailableFeatureFlag(status),
        [FEATURE_FLAGS.deploymentsPage]: unavailableFeatureFlag(status),
        [FEATURE_FLAGS.riskWatchdog]: unavailableFeatureFlag(status),
        [FEATURE_FLAGS.explore]: unavailableFeatureFlag(status),
      };

      const { result } = renderHook(() => useProjectNavRoutes());
      const navRoutes = result.current.map((entry) => entry.route);

      expect(navRoutes).not.toContain(routes.assistants);
      expect(navRoutes).not.toContain(routes.watchdog);
      expect(navRoutes).not.toContain(routes.explore);
      expect(navRoutes).toContain(routes.deployments);
      // Without Watchdog, the legacy risk pages stay in the nav.
      expect(navRoutes).toContain(routes.riskOverview);
      expect(navRoutes).toContain(routes.riskEvents);
    },
  );

  it("keeps Explore out of the nav until its flag is released to the organization", () => {
    const { result: hidden } = renderHook(() => useProjectNavRoutes());
    expect(hidden.current.map((entry) => entry.route)).not.toContain(
      routes.explore,
    );

    testState.featureFlags = {
      ...testState.featureFlags,
      [FEATURE_FLAGS.explore]: { status: "enabled" },
    };
    const { result: shown } = renderHook(() => useProjectNavRoutes());
    expect(shown.current.map((entry) => entry.route)).toContain(routes.explore);
  });

  it("uses resolved values for feature-gated navigation", () => {
    testState.featureFlags = {
      [FEATURE_FLAGS.agentManagement]: { status: "enabled" },
      [FEATURE_FLAGS.userSessionsDashboard]: { status: "enabled" },
      [FEATURE_FLAGS.assistants]: { status: "enabled" },
      [FEATURE_FLAGS.deploymentsPage]: { status: "disabled" },
      [FEATURE_FLAGS.riskWatchdog]: { status: "enabled" },
      [FEATURE_FLAGS.explore]: { status: "enabled" },
    };

    const { result } = renderHook(() => useProjectNavRoutes());
    const navRoutes = result.current.map((entry) => entry.route);

    expect(navRoutes).toContain(routes.assistants);
    expect(navRoutes).toContain(routes.watchdog);
    expect(navRoutes).toContain(routes.explore);
    expect(navRoutes).not.toContain(routes.deployments);
    // Watchdog supersedes the legacy overview in the nav; Risk Events shows
    // in both modes.
    expect(navRoutes).not.toContain(routes.riskOverview);
    expect(navRoutes).toContain(routes.riskEvents);
  });
});
