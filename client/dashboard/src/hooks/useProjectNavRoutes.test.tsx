import { renderHook } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRoutes } from "@/routes";
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

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

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
    [FEATURE_FLAGS.explore]: unavailableFeatureFlag("loading"),
  };
});

// Routes are compared by href: each useRoutes call builds its own objects.
let routes: ReturnType<typeof useRoutes>;
beforeAll(() => {
  routes = renderHook(() => useRoutes(), { wrapper: MemoryRouter }).result
    .current;
});

const renderNav = () =>
  renderHook(() => useProjectNavRoutes(), { wrapper: MemoryRouter });

describe("useProjectNavRoutes", () => {
  it("removes Signals intelligence when entitlement is disabled or a refresh fails", () => {
    testState.productFeatures.data.signalsIntelligenceEnabled = true;
    const { result, rerender } = renderNav();
    const visible = () =>
      result.current.some(
        (entry) => entry.route.href() === routes.signalsIntelligence.href(),
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
    const { result } = renderNav();
    expect(result.current.map((entry) => entry.route.href())).toEqual(
      expect.arrayContaining([
        routes.identities.agents.href(),
        routes.mcpSessions.href(),
        routes.remoteIdentityProviders.href(),
      ]),
    );
  });

  it("lists the two rosters and keeps the old agents page out of nav", () => {
    const { result } = renderNav();

    expect(
      result.current.find(
        (entry) => entry.route.href() === routes.agents.href(),
      ),
    ).toBeUndefined();
    expect(
      result.current.find(
        (entry) => entry.route.href() === routes.identities.people.href(),
      )?.scope,
    ).toEqual(["project:read"]);
    expect(
      result.current.find(
        (entry) => entry.route.href() === routes.identities.agents.href(),
      ),
    ).toBeTruthy();
  });

  it("uses the selected project's read grant for MCP Sessions", () => {
    const { result, rerender } = renderNav();
    const sessions = () =>
      result.current.find(
        (entry) => entry.route.href() === routes.mcpSessions.href(),
      );
    expect(sessions()?.scope).toEqual(["project:read"]);
    expect(sessions()?.resourceId).toBe("project_a");
    testState.projectId = "project_b";
    rerender();
    expect(sessions()?.resourceId).toBe("project_b");
  });

  it("keeps Agent Sessions discoverable for members who can read their own history", () => {
    const { result } = renderNav();
    const sessions = result.current.find(
      (entry) => entry.route.href() === routes.agentSessions.href(),
    );
    // Command K registers these routes as page actions. Requiring chat:read
    // here would hide the search destination from owners without that grant.
    expect(sessions?.scope).toEqual(["project:read"]);
  });

  it("lists Identity before MCP Gateway, Security and Policy, and Observability", () => {
    const { result } = renderNav();
    const navRoutes = result.current.map((entry) => entry.route.href());
    expect(navRoutes.slice(2, 6)).toEqual([
      routes.identities.people.href(),
      routes.identities.agents.href(),
      routes.mcpSessions.href(),
      routes.remoteIdentityProviders.href(),
    ]);
    expect(navRoutes.indexOf(routes.playground.href())).toBeLessThan(
      navRoutes.indexOf(routes.riskEvents.href()),
    );
    expect(navRoutes.indexOf(routes.policyCenter.href())).toBeLessThan(
      navRoutes.indexOf(routes.costs.href()),
    );
  });

  it("leaves Shadow AI and Shadow MCP out of nav; they live in Guardrails", () => {
    const { result } = renderNav();

    const titles = result.current.map((entry) => entry.route.title);
    expect(titles).not.toContain("Shadow AI");
    expect(titles).not.toContain("Shadow MCP");
    expect(titles).toContain("Guardrails");
  });

  it("leaves Approval Requests out of nav", () => {
    const { result } = renderNav();

    const navTitles = result.current.map((entry) => entry.route.title);

    expect(navTitles).not.toContain("Approval Requests");
  });

  it("uses project-scoped skill read for Skills", () => {
    const { result } = renderNav();
    const skills = result.current.find(
      (entry) => entry.route.href() === routes.skills.href(),
    );

    expect(skills?.scope).toEqual(["skill:read"]);
    expect(skills?.resourceId).toBe("project_a");
  });

  it("only includes Org Memory when its session toggle is enabled", () => {
    testState.orgMemoryEnabled = false;
    const { result, rerender } = renderNav();

    expect(
      result.current.some(
        (entry) => entry.route.href() === routes.orgMemory.href(),
      ),
    ).toBe(false);

    testState.orgMemoryEnabled = true;
    rerender();

    expect(
      result.current.some(
        (entry) => entry.route.href() === routes.orgMemory.href(),
      ),
    ).toBe(true);
  });

  it.each(["loading", "missing", "error"] as const)(
    "keeps standard navigation while rollout flags are %s",
    (status) => {
      testState.featureFlags = {
        [FEATURE_FLAGS.explore]: unavailableFeatureFlag(status),
      };

      const { result } = renderNav();
      const navRoutes = result.current.map((entry) => entry.route.href());

      expect(navRoutes).toContain(routes.assistants.href());
      expect(navRoutes).toContain(routes.watchdog.href());
      expect(navRoutes).not.toContain(routes.explore.href());
      expect(navRoutes).toContain(routes.deployments.href());
      expect(navRoutes).toContain(routes.riskEvents.href());
    },
  );

  it("keeps Explore and Dashboards out of the nav until their flag is released to the organization", () => {
    const { result: hidden } = renderNav();
    const hiddenRoutes = hidden.current.map((entry) => entry.route.href());
    expect(hiddenRoutes).not.toContain(routes.explore.href());
    expect(hiddenRoutes).not.toContain(routes.dashboards.href());

    testState.featureFlags = {
      ...testState.featureFlags,
      [FEATURE_FLAGS.explore]: { status: "enabled" },
    };
    const { result: shown } = renderNav();
    const shownRoutes = shown.current.map((entry) => entry.route.href());
    expect(shownRoutes).toContain(routes.explore.href());
    expect(shownRoutes).toContain(routes.dashboards.href());
  });

  it("requires organization admin for Watchdog navigation, matching its page", () => {
    const { result } = renderNav();
    const watchdog = result.current.find(
      (entry) => entry.route.href() === routes.watchdog.href(),
    );

    expect(watchdog?.scope).toEqual(["org:admin"]);
  });

  it("uses resolved values for feature-gated navigation", () => {
    testState.featureFlags = {
      [FEATURE_FLAGS.explore]: { status: "enabled" },
    };

    const { result } = renderNav();
    const navRoutes = result.current.map((entry) => entry.route.href());

    expect(navRoutes).toContain(routes.assistants.href());
    expect(navRoutes).toContain(routes.watchdog.href());
    expect(navRoutes).toContain(routes.explore.href());
    expect(navRoutes).toContain(routes.deployments.href());
    expect(navRoutes).toContain(routes.riskEvents.href());
  });

  it("uses assistant read on the active project for assistant surfaces", () => {
    const { result } = renderNav();
    const assistantRoutes = result.current.filter(
      (entry) =>
        entry.route.href() === routes.chat.href() ||
        entry.route.href() === routes.assistants.href(),
    );

    expect(assistantRoutes).toHaveLength(2);
    expect(assistantRoutes).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          route: expect.objectContaining({ title: routes.chat.title }),
          scope: ["assistant:read"],
          projectId: "project_a",
        }),
        expect.objectContaining({
          route: expect.objectContaining({ title: routes.assistants.title }),
          scope: ["assistant:read"],
          projectId: "project_a",
        }),
      ]),
    );
  });
});
