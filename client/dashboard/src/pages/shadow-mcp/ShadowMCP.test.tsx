import { cleanup, render, screen } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ShadowMCP from "./ShadowMCP";

const mocks = vi.hoisted(() => ({
  useProject: vi.fn(),
  useRBAC: vi.fn(),
  useRiskListPolicies: vi.fn(),
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
        <div>
          {title}
          {cta}
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

vi.mock("@gram/client/react-query/index.js", () => ({
  useRiskListPolicies: mocks.useRiskListPolicies,
}));

vi.mock("@speakeasy-api/moonshine", () => ({
  Badge: Object.assign(
    ({ children }: { children: ReactNode }) => <span>{children}</span>,
    {
      Text: ({ children }: { children: ReactNode }) => <span>{children}</span>,
    },
  ),
}));

vi.mock("@/components/shadow-mcp/ShadowMCPInventoryTable", () => ({
  ShadowMCPInventoryTable: ({
    policyState,
    projectID,
  }: {
    policyState: string;
    projectID: string;
  }) => (
    <div>
      Shadow MCP inventory for {projectID} with policy {policyState}
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
    sources = ["shadow_mcp"],
  }: {
    action: "block" | "flag";
    enabled?: boolean;
    sources?: string[];
  }) {
    return { action, enabled, sources };
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
  });

  it("renders the Shadow MCP inventory management page", () => {
    render(<ShadowMCP />);

    expect(screen.getByRole("heading", { name: "Shadow MCP" })).toBeTruthy();
    expect(
      screen.getByText(
        "Manage project-scoped Shadow MCP server inventory and URL access rules.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("No policy")).toBeTruthy();
    expect(
      screen.getByText("Shadow MCP inventory for project-1 with policy none"),
    ).toBeTruthy();
  });

  it("renders blocking policy status in the page action slot", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: {
        policies: [
          riskPolicy({ action: "flag" }),
          riskPolicy({ action: "block" }),
        ],
      },
      isError: false,
      isLoading: false,
    });

    render(<ShadowMCP />);

    expect(screen.getByText("Blocking enabled")).toBeTruthy();
    expect(
      screen.getByText(
        "Shadow MCP inventory for project-1 with policy blocking",
      ),
    ).toBeTruthy();
  });

  it("renders flagging policy status when no blocking policy is enabled", () => {
    mocks.useRiskListPolicies.mockReturnValue({
      data: { policies: [riskPolicy({ action: "flag" })] },
      isError: false,
      isLoading: false,
    });

    render(<ShadowMCP />);

    expect(screen.getByText("Flagging enabled")).toBeTruthy();
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

    expect(screen.getByText("No policy")).toBeTruthy();
    expect(
      screen.getByText("Shadow MCP inventory for project-1 with policy none"),
    ).toBeTruthy();
  });
});
