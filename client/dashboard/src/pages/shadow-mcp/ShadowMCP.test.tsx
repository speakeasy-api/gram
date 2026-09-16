import { cleanup, render, screen } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { MemoryRouter } from "react-router";
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

vi.mock("@/components/ui/Skeleton", () => ({
  SkeletonTable: () => <div>Loading table</div>,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    shadowAI: {
      harnesses: { href: () => "/shadow-ai/harnesses" },
      assistants: { href: () => "/shadow-ai/assistants" },
      models: { href: () => "/shadow-ai/models" },
      mcps: {
        href: () => "/shadow-ai/mcps",
        detail: {
          href: (serverURL: string) => `/shadow-ai/mcps/${serverURL}`,
          goTo: () => undefined,
        },
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
    // The tab is reachable at project read now; org admin still gates every
    // action on it.
    const granted = ["org:admin", "project:read", "project:write"];
    mocks.useRBAC.mockReturnValue({
      hasAnyScope: (scopes: string[]) =>
        scopes.some((scope) => granted.includes(scope)),
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
    render(
      <MemoryRouter>
        <ShadowMCP />
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { name: "MCPs" })).toBeTruthy();
    expect(
      screen.getByText(/Every MCP server this project knows about/),
    ).toBeTruthy();
    expect(screen.getByText("Shadow MCP inventory for project-1")).toBeTruthy();
  });

  it("blocks inventory rendering until policy data loads", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: undefined,
      isError: false,
      isLoading: true,
    });

    render(
      <MemoryRouter>
        <ShadowMCP />
      </MemoryRouter>,
    );

    expect(screen.getByRole("status").getAttribute("aria-label")).toBe(
      "Loading Shadow MCP policies",
    );
    expect(screen.getByText("Loading table")).toBeTruthy();
    expect(screen.queryByText(/Shadow MCP inventory for/)).toBeNull();
  });

  it("renders with different policy configurations", () => {
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
    });

    render(
      <MemoryRouter>
        <ShadowMCP />
      </MemoryRouter>,
    );

    expect(screen.getByText("Shadow MCP inventory for project-1")).toBeTruthy();
    expect(
      screen.getByText("Shadow MCP policies: block-policy-1"),
    ).toBeTruthy();
    expect(screen.getByText("Roles: Admin")).toBeTruthy();
    expect(screen.getByText("Members: Admin User")).toBeTruthy();
  });
});
