import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AppRoute } from "@/routes";
import { useProjectNavRoutes } from "./useProjectNavRoutes";

const testState = vi.hoisted(() => ({
  productFeatureOptions: undefined as
    | { staleTime?: number; throwOnError?: boolean }
    | undefined,
  projectId: "project_a",
  skillsEnabled: false,
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
  playground: route("Playground", "playground"),
  plugins: route("Plugins", "plugins"),
  policyCenter: route("Risk Policies", "risk-policies"),
  riskEvents: route("Risk Events", "risk-events"),
  riskOverview: route("Risk Overview", "risk"),
  settings: route("Project settings", "settings"),
  shadowMCP: route("Shadow MCP", "shadow-mcp"),
  sources: route("Sources", "sources"),
};

vi.mock("@/routes", async () => {
  return {
    useRoutes: () => routes,
  };
});

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({
    isFeatureEnabled: () => false,
  }),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: testState.projectId }),
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: (
    _request: unknown,
    _security: unknown,
    options: { staleTime?: number; throwOnError?: boolean } | undefined,
  ) => {
    testState.productFeatureOptions = options;
    return { data: { skillsEnabled: testState.skillsEnabled } };
  },
}));

describe("useProjectNavRoutes", () => {
  it("uses Shadow MCP as the sidebar destination while leaving Approval Requests out of nav", () => {
    const { result } = renderHook(() => useProjectNavRoutes());

    const navTitles = result.current.map((entry) => entry.route.title);

    expect(navTitles).toContain("Shadow MCP");
    expect(navTitles).not.toContain("Approval Requests");
  });

  it("uses project read for Skills when the product feature is disabled", () => {
    testState.skillsEnabled = false;

    const { result } = renderHook(() => useProjectNavRoutes());
    const skills = result.current.find(
      (entry) => entry.route === routes.skills,
    );

    expect(skills?.scope).toEqual(["project:read"]);
    expect(skills?.resourceId).toBeUndefined();
  });

  it("uses skill read for Skills when the product feature is enabled", () => {
    testState.skillsEnabled = true;

    const { result } = renderHook(() => useProjectNavRoutes());
    const skills = result.current.find(
      (entry) => entry.route === routes.skills,
    );

    expect(skills?.scope).toEqual(["skill:read"]);
    expect(skills?.resourceId).toBe("project_a");
    expect(testState.productFeatureOptions?.staleTime).toBe(30_000);
    expect(testState.productFeatureOptions?.throwOnError).toBe(false);
  });
});
