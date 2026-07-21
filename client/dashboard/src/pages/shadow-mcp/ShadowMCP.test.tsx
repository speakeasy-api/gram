import { cleanup, render, screen, within } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ShadowMCP from "./ShadowMCP";

const mocks = vi.hoisted(() => ({
  useProject: vi.fn(),
  useRBAC: vi.fn(),
  useMembers: vi.fn(),
  useRiskListPolicies: vi.fn(),
  useRoles: vi.fn(),
}));

vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <div>{children}</div>;
  }

  function Header({ children }: { children?: ReactNode }) {
    return <div>{children}</div>;
  }
  Header.Breadcrumbs = () => null;

  function Body({ children }: { children: ReactNode }) {
    return <div>{children}</div>;
  }

  function Section({ children }: { children: ReactNode }) {
    let title: ReactElement | null = null;
    let description: ReactElement | null = null;
    let cta: ReactElement | null = null;
    let body: ReactElement | null = null;

    for (const child of Array.isArray(children) ? children : [children]) {
      if (typeof child === "object" && child && "type" in child) {
        if (child.type === Section.Title) title = child;
        if (child.type === Section.Description) description = child;
        if (child.type === Section.CTA) cta = child;
        if (child.type === Section.Body) body = child;
      }
    }

    return (
      <section>
        <div data-testid="section-header">
          {title}
          <div data-testid="section-cta">{cta}</div>
        </div>
        {description}
        {body}
      </section>
    );
  }
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h1>{children}</h1>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.CTA = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;

  return {
    Page: Object.assign(Page, {
      Header,
      Body,
      Section,
    }),
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

vi.mock("@/components/ui/skeleton", () => ({
  SkeletonTable: () => <div>Loading table</div>,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    shadowMCP: {
      detail: {
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
    policyState,
    projectID,
  }: {
    members: Array<{ name: string }>;
    roles: Array<{ name: string }>;
    shadowMCPPolicies: Array<{ id: string }>;
    policyState: string;
    projectID: string;
  }) => (
    <div>
      Shadow MCP inventory for {projectID} with policy {policyState}
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
    action: "block" | "flag";
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
    mocks.useMembers.mockReturnValue({
      data: { members: [{ name: "Admin User" }] },
    });
    mocks.useRoles.mockReturnValue({
      data: { roles: [{ name: "Admin" }] },
    });
  });

  it("renders the Shadow MCP inventory management page", () => {
    render(<ShadowMCP />);

    expect(screen.getByRole("heading", { name: "Shadow MCP" })).toBeTruthy();
    expect(
      screen.getByText(
        "Manage the Shadow MCP server inventory, allow decisions, and requests.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("No Policy")).toBeTruthy();
    expect(
      screen.getByText(
        "No policy is enabled. All Shadow MCP servers are allowed.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("Shadow MCP inventory for project-1 with policy none"),
    ).toBeTruthy();
  });

  it("blocks inventory rendering until policy data loads", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: undefined,
      isError: false,
      isLoading: true,
    });

    render(<ShadowMCP />);

    expect(screen.getByRole("status").getAttribute("aria-label")).toBe(
      "Loading Shadow MCP policies",
    );
    expect(screen.getByText("Loading table")).toBeTruthy();
    expect(screen.queryByText("No Policy")).toBeNull();
    expect(screen.queryByText(/Shadow MCP inventory for/)).toBeNull();
    expect(
      within(screen.getByTestId("section-cta")).queryByText(/./),
    ).toBeNull();
  });

  it("renders blocking policy status in the section header", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: {
        policies: [
          riskPolicy({ action: "flag" }),
          riskPolicy({ action: "block", id: "block-policy-1" }),
        ],
      },
      isError: false,
      isLoading: false,
    });

    render(<ShadowMCP />);

    const sectionCTA = within(screen.getByTestId("section-cta"));
    expect(sectionCTA.getByText("Blocking")).toBeTruthy();
    expect(
      sectionCTA.getByText(
        "Block policy is enabled. Servers without allow rules are not allowed.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Shadow MCP inventory for project-1 with policy blocking",
      ),
    ).toBeTruthy();
    // Only enabled blocking policies are eligible for allow rules; the flag
    // policy must not be offered in the inventory actions.
    expect(
      screen.getByText("Shadow MCP policies: block-policy-1"),
    ).toBeTruthy();
    expect(screen.getByText("Roles: Admin")).toBeTruthy();
    expect(screen.getByText("Members: Admin User")).toBeTruthy();
  });

  it("renders flagging policy status when no blocking policy is enabled", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: { policies: [riskPolicy({ action: "flag" })] },
      isError: false,
      isLoading: false,
    });

    render(<ShadowMCP />);

    expect(screen.getByText("Flagging")).toBeTruthy();
    expect(
      screen.getByText(
        "Flagging policy is enabled. Servers without allow rules are only flagged.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Shadow MCP inventory for project-1 with policy flagging",
      ),
    ).toBeTruthy();
  });

  it("renders no policy when no enabled Shadow MCP policy exists", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: {
        policies: [
          riskPolicy({ action: "block", enabled: false }),
          riskPolicy({ action: "block", sources: ["prompt_injection"] }),
        ],
      },
      isError: false,
      isLoading: false,
    });

    render(<ShadowMCP />);

    expect(screen.getByText("No Policy")).toBeTruthy();
    expect(
      screen.getByText("Shadow MCP inventory for project-1 with policy none"),
    ).toBeTruthy();
    expect(screen.getByText("Shadow MCP policies: none")).toBeTruthy();
  });
});
