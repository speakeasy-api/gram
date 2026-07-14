import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AppRoute } from "@/routes";
import { useProjectNavRoutes } from "./useProjectNavRoutes";

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
  clis: route("CLIs", "clis"),
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

describe("useProjectNavRoutes", () => {
  it("uses Shadow MCP as the sidebar destination while leaving Approval Requests out of nav", () => {
    const { result } = renderHook(() => useProjectNavRoutes());

    const navTitles = result.current.map((entry) => entry.route.title);

    expect(navTitles).toContain("Shadow MCP");
    expect(navTitles).not.toContain("Approval Requests");
  });
});
