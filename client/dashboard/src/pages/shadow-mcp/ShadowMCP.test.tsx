import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ShadowMCP from "./ShadowMCP";
import { testAccessSummary } from "@/components/shadow-mcp/shadowMCPInventoryTestFixtures";

const mocks = vi.hoisted(() => ({
  useProject: vi.fn(),
  useRBAC: vi.fn(),
  useShadowMCPPolicyInventory: vi.fn(),
  useMembers: vi.fn(),
  useRiskListPolicies: vi.fn(),
  useRoles: vi.fn(),
}));

vi.mock("@/components/page-templates", () => {
  return {
    TabbedPage: ({
      title,
      tabs,
      activeTab,
      children,
    }: {
      title: string;
      tabs: Array<{ value: string; label: string }>;
      activeTab: string;
      children: ReactNode;
    }) => (
      <section>
        <h1>{title}</h1>
        <div data-testid="tabs">
          {tabs.map((tab) => (
            <span key={tab.value}>{tab.label}</span>
          ))}
        </div>
        <div data-testid="active-tab">{activeTab}</div>
        {children}
      </section>
    ),
  };
});

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: mocks.useRiskListPolicies,
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: mocks.useMembers,
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: mocks.useRoles,
}));

vi.mock("@/components/shadow-mcp/useShadowMCPPolicyInventory", () => ({
  useShadowMCPPolicyInventory: mocks.useShadowMCPPolicyInventory,
}));

vi.mock("@/components/ui/Skeleton", () => ({
  SkeletonTable: () => <div>Loading table</div>,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { goTo: vi.fn(), href: () => "/mcp" },
    policyCenter: { goTo: vi.fn(), href: () => "/risk-policies" },
    shadowMCP: {
      detail: {
        goTo: vi.fn(),
        href: (serverURL: string) => `/shadow-mcp/${serverURL}`,
      },
    },
  }),
}));

vi.mock("@/components/shadow-mcp/ShadowMCPInventoryTable", () => ({
  ShadowMCPInventoryTable: ({
    members,
    roles,
    shadowMCPPolicies,
    projectID,
  }: {
    members: Array<{ name: string }>;
    roles: Array<{ name: string }>;
    shadowMCPPolicies: Array<{ id: string }>;
    projectID: string;
  }) => (
    <div>
      Shadow MCP inventory for {projectID}
      <span>
        Shadow MCP policies:{" "}
        {shadowMCPPolicies.map((policy) => policy.id).join(",") || "none"}
      </span>
      <span>Roles: {roles.map((role) => role.name).join(",") || "none"}</span>
      <span>
        Members: {members.map((member) => member.name).join(",") || "none"}
      </span>
    </div>
  ),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: mocks.useProject,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: mocks.useRBAC,
}));

describe("ShadowMCP", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  function riskPolicy({
    action,
    enabled = true,
    id = `${action}-policy`,
    sources = ["shadow_mcp"],
  }: {
    action: "block" | "flag" | "warn";
    enabled?: boolean;
    id?: string;
    sources?: string[];
  }) {
    return { action, enabled, id, sources };
  }

  beforeEach(() => {
    mocks.useProject.mockReturnValue({
      id: "project-1",
      name: "Demo",
      slug: "demo",
    });
    mocks.useRBAC.mockReturnValue({
      hasAnyScope: (scopes: string[]) => scopes.includes("org:admin"),
      hasAllScopes: () => true,
      isLoading: false,
    });
    mocks.useRiskListPolicies.mockReturnValue({
      data: { policies: [] },
      isError: false,
      isLoading: false,
    });
    mocks.useShadowMCPPolicyInventory.mockReturnValue({
      data: [],
      isPending: false,
      isError: false,
      refetch: vi.fn(),
    });
    mocks.useMembers.mockReturnValue({
      data: { members: [{ name: "Admin User" }] },
    });
    mocks.useRoles.mockReturnValue({
      data: { roles: [{ name: "Admin" }] },
    });
  });

  function inventoryServer(overrides: Record<string, unknown> = {}) {
    return {
      access: "none",
      accessSummary: testAccessSummary("none"),
      allowedPolicyIds: [],
      blockedPolicyIds: [],
      canonicalServerUrl: "https://shadow.example/mcp",
      firstSeen: new Date("2026-01-01T00:00:00Z"),
      lastCalled: new Date("2026-01-03T00:00:00Z"),
      lastSeen: new Date("2026-01-03T00:00:00Z"),
      observedUseCount: 12,
      requestCount: 0,
      serverName: "Shadow Example",
      serverSlug: "shadow-example",
      topUsers: [],
      urlHost: "shadow.example",
      userCount: 2,
      ...overrides,
    };
  }

  it("defaults to the policy use case tab", () => {
    render(
      <MemoryRouter>
        <ShadowMCP />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("heading", { name: "Shadow MCP Inventory" }),
    ).toBeTruthy();
    expect(screen.getByTestId("active-tab").textContent).toBe("policy");
    expect(screen.getByText("Policy Use Case")).toBeTruthy();
    expect(screen.getByText("Gateway Use Case")).toBeTruthy();
    expect(screen.getByText("Full Inventory")).toBeTruthy();
    expect(screen.getByText("Top risky Shadow MCP servers")).toBeTruthy();
    expect(screen.getByText("Open Guardrails")).toBeTruthy();
  });

  it("shows loading state while policy use case inventory is pending", () => {
    mocks.useShadowMCPPolicyInventory.mockReturnValue({
      data: undefined,
      isPending: true,
      isError: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <ShadowMCP />
      </MemoryRouter>,
    );

    expect(screen.getByRole("status").getAttribute("aria-label")).toBe(
      "Loading risky Shadow MCP servers",
    );
    expect(screen.getByText("Loading table")).toBeTruthy();
  });

  it("renders inventory tab with policy status and table", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: {
        policies: [riskPolicy({ action: "block", id: "block-policy-1" })],
      },
      isError: false,
      isLoading: false,
    });
    mocks.useShadowMCPPolicyInventory.mockReturnValue({
      data: [inventoryServer()],
      isPending: false,
      isError: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter
        initialEntries={["/org/projects/demo/shadow-mcp?tab=inventory"]}
      >
        <ShadowMCP />
      </MemoryRouter>,
    );

    expect(screen.getByTestId("active-tab").textContent).toBe("inventory");
    expect(screen.getByText("Blocking")).toBeTruthy();
    expect(screen.getByText("Shadow MCP inventory for project-1")).toBeTruthy();
    expect(
      screen.getByText("Shadow MCP policies: block-policy-1"),
    ).toBeTruthy();
    expect(screen.getByText("Roles: Admin")).toBeTruthy();
    expect(screen.getByText("Members: Admin User")).toBeTruthy();
  });
});
