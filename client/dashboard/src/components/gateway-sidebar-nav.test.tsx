import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";

import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { GatewaySidebarNav } from "./gateway-sidebar-nav";

const DEBUG_HREF = "/acme/projects/first/mcp/gateway/g1/debug";

const mocks = vi.hoisted(() => ({
  rollout: { status: "enabled" } as FeatureFlagResult,
  scopes: ["project:read"] as string[],
  debugAction: vi.fn(),
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.rollout,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => mocks.scopes.includes(scope),
  }),
}));
vi.mock("@/pages/mcp/gateway/useGatewayDebugAction", () => ({
  useGatewayDebugAction: mocks.debugAction,
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "p1", slug: "first" }),
}));
vi.mock("react-router", () => ({
  useLocation: () => ({
    pathname: "/acme/projects/first/mcp/gateway/g1/overview",
  }),
  useParams: () => ({ gatewayId: "g1" }),
}));
vi.mock("@/routes", () => {
  const tab = (name: string) => ({
    href: (id: string) => `/acme/projects/first/mcp/gateway/${id}/${name}`,
  });
  return {
    useRoutes: () => ({
      mcp: {
        href: () => "/acme/projects/first/mcp",
        gateway: {
          overview: tab("overview"),
          inspect: tab("inspect"),
          teamAccess: tab("team-access"),
          sessions: tab("sessions"),
          debug: tab("debug"),
          settings: tab("settings"),
        },
      },
    }),
  };
});
vi.mock("@gram/client/react-query/getMetaMcpServer.js", () => ({
  useGetMetaMcpServer: () => ({
    data: { id: "g1", name: "Gateway", userSessionIssuerId: null },
  }),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({ data: { mcpEndpoints: [] }, isLoading: false }),
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  gatewayInstallPageUrl: () => null,
  useResolvedMcpServerUrl: () => ({ mcpUrl: null }),
}));
vi.mock("@/pages/mcp/gateway/useGatewayMemberRows", () => ({
  useGatewayMemberRows: () => ({ rows: [] }),
}));
vi.mock("@/pages/mcp/gateway/GatewaySettingsTab", () => ({
  GATEWAY_AUTHENTICATION_SECTION_ID: "authentication",
}));
vi.mock("@/pages/mcp/x/tabs/settings/sections/ServerUrlSection", () => ({
  MCP_SERVER_URL_SECTION_ID: "server-url",
}));
vi.mock("@/components/mcp-server-readiness-bar", () => ({
  McpServerReadinessBar: () => null,
}));
vi.mock("@/components/detail/detail-sidebar-nav", () => ({
  DetailSidebarInfoLabel: ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  ),
  DetailSidebarNav: ({
    items,
  }: {
    items: { key: string; title: string }[];
  }) => (
    <nav>
      {items.map((item) => (
        <a key={item.key}>{item.title}</a>
      ))}
    </nav>
  ),
}));

afterEach(() => {
  cleanup();
  mocks.rollout = { status: "enabled" };
  mocks.scopes = ["project:read"];
  mocks.debugAction.mockClear();
});

it("offers Debug in the sidebar and Command K when rolled out and readable", () => {
  render(<GatewaySidebarNav />);
  expect(screen.getByText("Debug")).toBeTruthy();
  expect(mocks.debugAction).toHaveBeenLastCalledWith(DEBUG_HREF);
});

it.each(["loading", "disabled", "missing", "error"] as const)(
  "offers no Debug entry or action while the rollout is %s",
  (status) => {
    mocks.rollout = { status };
    render(<GatewaySidebarNav />);
    expect(screen.queryByText("Debug")).toBeNull();
    expect(mocks.debugAction).toHaveBeenLastCalledWith(null);
  },
);

it("offers no Debug entry or action without project read access", () => {
  mocks.scopes = [];
  render(<GatewaySidebarNav />);
  expect(screen.queryByText("Debug")).toBeNull();
  expect(mocks.debugAction).toHaveBeenLastCalledWith(null);
});
