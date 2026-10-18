import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MCPApprovalsTab } from "./MCPApprovalsTab";

const mocks = vi.hoisted(() => ({
  useProject: vi.fn(),
  useMembers: vi.fn(),
  useRiskListPolicies: vi.fn(),
  useRoles: vi.fn(),
  goToServer: vi.fn(),
  refetchPolicies: vi.fn(),
  insightsConfig: vi.fn(),
}));

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: mocks.useRiskListPolicies,
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: mocks.useMembers,
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: mocks.useRoles,
}));

vi.mock("@/components/ui/Skeleton", () => ({
  SkeletonTable: () => <div>Loading table</div>,
}));

// The dock needs the insights provider; the tab test only checks what it
// is told.
vi.mock("@/components/insights-dock", () => ({
  InsightsConfig: (props: { title: string; contextInfo: string }) => {
    mocks.insightsConfig(props);
    return null;
  },
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    shadowAI: {
      mcps: {
        detail: {
          href: (serverSlug: string) => `/shadow-ai/mcps/${serverSlug}`,
          goTo: mocks.goToServer,
        },
      },
    },
  }),
}));

// The table's own behavior is covered by its dedicated tests; this file
// asserts the wiring, so a marker records the props it receives and offers a
// row to open.
vi.mock("@/components/shadow-mcp/ShadowMCPInventoryTable", () => ({
  ShadowMCPInventoryTable: ({
    members,
    onOpenServer,
    policiesUnavailable,
    roles,
    shadowMCPPolicies,
    projectID,
  }: {
    members: Array<{ name: string }>;
    onOpenServer?: (server: { serverSlug: string }) => void;
    policiesUnavailable?: boolean;
    roles: Array<{ name: string }>;
    shadowMCPPolicies: Array<{ id: string }>;
    projectID: string;
  }) => (
    <div
      data-testid="inventory-table"
      data-policies-unavailable={String(policiesUnavailable ?? false)}
    >
      Shadow MCP inventory for {projectID}
      <span>
        Shadow MCP policies:{" "}
        {shadowMCPPolicies.map((policy) => policy.id).join(",") || "none"}
      </span>
      <span>Roles: {roles.map((role) => role.name).join(",") || "none"}</span>
      <span>
        Members: {members.map((member) => member.name).join(",") || "none"}
      </span>
      <button
        type="button"
        onClick={() => onOpenServer?.({ serverSlug: "server-slug" })}
      >
        Open server
      </button>
    </div>
  ),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: mocks.useProject,
}));

describe("MCPApprovalsTab", () => {
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
    mocks.useRiskListPolicies.mockReturnValue({
      data: { policies: [] },
      isError: false,
      isLoading: false,
      refetch: mocks.refetchPolicies,
    });
    mocks.useMembers.mockReturnValue({
      data: { members: [{ name: "Admin User" }] },
    });
    mocks.useRoles.mockReturnValue({
      data: { roles: [{ name: "Admin" }] },
    });
  });

  it("renders the inventory with the policy status beside the heading", () => {
    render(
      <MemoryRouter>
        <MCPApprovalsTab />
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { name: "MCP approvals" })).toBeTruthy();
    expect(
      screen.getByText(/Every MCP server this project knows about/),
    ).toBeTruthy();
    expect(screen.getByText("No Policy")).toBeTruthy();
    expect(
      screen.getByText(
        "No policy is enabled. All Shadow MCP servers are allowed.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Shadow MCP inventory for project-1")).toBeTruthy();
    expect(screen.getByText("Shadow MCP policies: none")).toBeTruthy();
    expect(
      screen
        .getByTestId("inventory-table")
        .getAttribute("data-policies-unavailable"),
    ).toBe("false");
    expect(screen.queryByText(/Policy data could not be loaded/)).toBeNull();
  });

  it("tells the assistant which tab it is on and the policy posture", () => {
    render(
      <MemoryRouter>
        <MCPApprovalsTab />
      </MemoryRouter>,
    );

    expect(mocks.insightsConfig).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "MCP approval insights",
        contextInfo: expect.stringContaining(
          "Page: Guardrails, MCP approvals tab. Shadow MCP policy posture: none.",
        ),
      }),
    );
  });

  it("holds the inventory until the policy, member and role reads settle", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: undefined,
      isError: false,
      isLoading: true,
      refetch: mocks.refetchPolicies,
    });

    render(
      <MemoryRouter>
        <MCPApprovalsTab />
      </MemoryRouter>,
    );

    expect(screen.getByRole("status").getAttribute("aria-label")).toBe(
      "Loading Shadow MCP policies",
    );
    expect(screen.getByText("Loading table")).toBeTruthy();
    expect(screen.queryByText("No Policy")).toBeNull();
    expect(screen.queryByText(/Shadow MCP inventory for/)).toBeNull();
  });

  it("passes the enabled blocking policies, members and roles to the table", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: {
        policies: [
          riskPolicy({ action: "flag" }),
          riskPolicy({ action: "block", enabled: false, id: "disabled-block" }),
          riskPolicy({ action: "block", id: "block-policy-1" }),
        ],
      },
      isError: false,
      isLoading: false,
      refetch: mocks.refetchPolicies,
    });

    render(
      <MemoryRouter>
        <MCPApprovalsTab />
      </MemoryRouter>,
    );

    expect(screen.getByText("Blocking")).toBeTruthy();
    expect(
      screen.getByText(
        "Block policy is enabled. Servers without allow rules are blocked for the policy's audience.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("Shadow MCP policies: block-policy-1"),
    ).toBeTruthy();
    expect(screen.getByText("Roles: Admin")).toBeTruthy();
    expect(screen.getByText("Members: Admin User")).toBeTruthy();
  });

  it("opens a server on its Shadow MCP server page", () => {
    render(
      <MemoryRouter>
        <MCPApprovalsTab />
      </MemoryRouter>,
    );

    screen.getByRole("button", { name: "Open server" }).click();

    expect(mocks.goToServer).toHaveBeenCalledWith("server-slug");
  });

  it("keeps the inventory browsable but pauses deciding when the policy list fails", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: undefined,
      isError: true,
      isLoading: false,
      refetch: mocks.refetchPolicies,
    });

    render(
      <MemoryRouter>
        <MCPApprovalsTab />
      </MemoryRouter>,
    );

    // No posture box: the posture is unknown, not "none".
    expect(screen.queryByText("No Policy")).toBeNull();
    expect(screen.getByText(/Policy data could not be loaded/)).toBeTruthy();
    expect(screen.getByText("Shadow MCP inventory for project-1")).toBeTruthy();
    // Deciding writes grants against the policies the sheet can see; with
    // none loaded the table must not offer it.
    expect(
      screen
        .getByTestId("inventory-table")
        .getAttribute("data-policies-unavailable"),
    ).toBe("true");

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.refetchPolicies).toHaveBeenCalledTimes(1);
  });
});
