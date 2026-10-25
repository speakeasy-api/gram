import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, expect, it, vi } from "vitest";

import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import GatewayDetails from "./GatewayDetails";

const mocks = vi.hoisted(() => ({
  rollout: { status: "enabled" } as FeatureFlagResult,
  canReadProject: true,
  debugMounts: 0,
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.rollout,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) =>
    mocks.canReadProject ? <>{children}</> : <p>Permission required</p>,
}));
vi.mock("./GatewayDebugTab", () => ({
  GatewayDebugTab: () => {
    mocks.debugMounts += 1;
    return <p>Debug content</p>;
  },
}));
vi.mock("@gram/client/react-query/getMetaMcpServer.js", () => ({
  useGetMetaMcpServer: () => ({
    data: { id: "g1", name: "Gateway", userSessionIssuerId: null },
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({ data: { mcpEndpoints: [] }, isLoading: false }),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "p1", slug: "first" }),
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
vi.mock("@/components/page-layout", () => {
  const Part = ({ children }: { children?: ReactNode }) => <>{children}</>;
  const Header = Object.assign(Part, { Breadcrumbs: () => null });
  return { Page: Object.assign(Part, { Header, Body: Part }) };
});
vi.mock("./GatewayOverviewTab", () => ({ GatewayOverviewTab: () => null }));
vi.mock("./GatewayInspectTab", () => ({ GatewayInspectTab: () => null }));
vi.mock("./GatewaySettingsTab", () => ({ GatewaySettingsTab: () => null }));
vi.mock("@/pages/mcp/MCPTeamAccessTab", () => ({
  MCPTeamAccessTab: () => null,
}));
vi.mock("@/components/sessions/ClientsAndSessionsTab", () => ({
  ClientsAndSessionsTab: () => null,
}));

afterEach(() => {
  cleanup();
  mocks.rollout = { status: "enabled" };
  mocks.canReadProject = true;
  mocks.debugMounts = 0;
});

function renderDebugRoute() {
  return render(
    <MemoryRouter
      initialEntries={["/acme/projects/first/mcp/gateway/g1/debug"]}
    >
      <Routes>
        <Route
          path="/:org/projects/:project/mcp/gateway/:gatewayId/*"
          element={<GatewayDetails />}
        />
      </Routes>
    </MemoryRouter>,
  );
}

it("opens Debug when the rollout is on and the viewer can read the project", () => {
  renderDebugRoute();
  expect(screen.getByText("Debug content")).toBeTruthy();
  expect(mocks.debugMounts).toBeGreaterThan(0);
});

it("mounts nothing while the rollout is still loading", () => {
  mocks.rollout = { status: "loading" };
  renderDebugRoute();
  expect(screen.queryByText("Debug content")).toBeNull();
  expect(screen.queryByText("Debug is not available yet")).toBeNull();
  expect(mocks.debugMounts).toBe(0);
});

it.each(["disabled", "missing", "error"] as const)(
  "stays closed when the rollout is %s",
  (status) => {
    mocks.rollout = { status };
    renderDebugRoute();
    expect(screen.getByText("Debug is not available yet")).toBeTruthy();
    expect(mocks.debugMounts).toBe(0);
  },
);

it("keeps the permission check whatever the rollout says", () => {
  mocks.canReadProject = false;
  renderDebugRoute();
  expect(screen.getByText("Permission required")).toBeTruthy();
  expect(screen.queryByText("Debug is not available yet")).toBeNull();
  expect(mocks.debugMounts).toBe(0);
});
