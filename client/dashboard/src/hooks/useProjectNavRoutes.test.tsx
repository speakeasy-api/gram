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
  agentSessions: route("Agent Sessions", "agent-sessions"),
  assistants: route("Assistants", "assistants"),
  catalog: route("Catalog", "catalog"),
  chat: route("Project Assistant", "chat"),
  skills: route("Skills", "skills"),
  costs: route("Costs", "costs"),
  deployments: route("Deployments", "deployments"),
  detectionRules: route("Detection Rules", "detection-rules"),
  employees: route("Employees", "employees"),
  environments: route("Environments", "environments"),
  home: route("Home", ""),
  insights: route("Insights", "insights"),
  logs: route("Logs", "logs"),
  mcp: route("MCP", "mcp"),
  orgMemory: route("Org Memory", "org-memory"),
  playground: route("Playground", "playground"),
  plugins: route("Plugins", "plugins"),
  policyCenter: route("Guardrails", "risk-policies"),
  riskEvents: route("Risk Events", "risk-events"),
  riskOverview: route("Risk Overview", "risk"),
  watchdog: route("Watchdog", "watchdog"),
  settings: route("Project settings", "settings"),
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
}));

vi.mock("./useOrgMemoryDeveloperToggle", () => ({
  useOrgMemoryDeveloperToggle: () => [testState.orgMemoryEnabled, vi.fn()],
}));

function unavailableFeatureFlag(
  status: "loading" | "missing" | "error",
): FeatureFlagResult {
  return { status };
}

beforeEach(() => {
  testState.projectId = "project_a";
  testState.orgMemoryEnabled = false;
  testState.featureFlags = {
    [FEATURE_FLAGS.assistants]: unavailableFeatureFlag("loading"),
    [FEATURE_FLAGS.deploymentsPage]: unavailableFeatureFlag("loading"),
    [FEATURE_FLAGS.riskWatchdog]: unavailableFeatureFlag("loading"),
  };
});

describe("useProjectNavRoutes", () => {
  it("uses Shadow MCP as the sidebar destination while leaving Approval Requests out of nav", () => {
    const { result } = renderHook(() => useProjectNavRoutes());

    const navTitles = result.current.map((entry) => entry.route.title);

    expect(navTitles).toContain("Shadow MCP");
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
        [FEATURE_FLAGS.assistants]: unavailableFeatureFlag(status),
        [FEATURE_FLAGS.deploymentsPage]: unavailableFeatureFlag(status),
        [FEATURE_FLAGS.riskWatchdog]: unavailableFeatureFlag(status),
      };

      const { result } = renderHook(() => useProjectNavRoutes());
      const navRoutes = result.current.map((entry) => entry.route);

      expect(navRoutes).not.toContain(routes.assistants);
      expect(navRoutes).not.toContain(routes.watchdog);
      expect(navRoutes).toContain(routes.deployments);
      // Without Watchdog, the legacy risk pages stay in the nav.
      expect(navRoutes).toContain(routes.riskOverview);
      expect(navRoutes).toContain(routes.riskEvents);
    },
  );

  it("uses resolved values for feature-gated navigation", () => {
    testState.featureFlags = {
      [FEATURE_FLAGS.assistants]: { status: "enabled" },
      [FEATURE_FLAGS.deploymentsPage]: { status: "disabled" },
      [FEATURE_FLAGS.riskWatchdog]: { status: "enabled" },
    };

    const { result } = renderHook(() => useProjectNavRoutes());
    const navRoutes = result.current.map((entry) => entry.route);

    expect(navRoutes).toContain(routes.assistants);
    expect(navRoutes).toContain(routes.watchdog);
    expect(navRoutes).not.toContain(routes.deployments);
    // Watchdog supersedes the legacy overview in the nav; Risk Events shows
    // in both modes.
    expect(navRoutes).not.toContain(routes.riskOverview);
    expect(navRoutes).toContain(routes.riskEvents);
  });
});
