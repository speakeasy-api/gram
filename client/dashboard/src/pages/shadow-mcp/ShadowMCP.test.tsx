import { cleanup, render, screen } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import ShadowMCP from "./ShadowMCP";

const mocks = vi.hoisted(() => ({
  useProject: vi.fn(),
  useRBAC: vi.fn(),
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
    let body: ReactElement | null = null;

    for (const child of Array.isArray(children) ? children : [children]) {
      if (typeof child === "object" && child && "type" in child) {
        if (child.type === Section.Title) title = child;
        if (child.type === Section.Description) description = child;
        if (child.type === Section.Body) body = child;
      }
    }

    return (
      <section>
        {title}
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
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;

  return {
    Page: Object.assign(Page, {
      Header,
      Body,
      Section,
    }),
  };
});

vi.mock("@/components/shadow-mcp/ShadowMCPInventoryTable", () => ({
  ShadowMCPInventoryTable: ({ projectID }: { projectID: string }) => (
    <div>Shadow MCP inventory for {projectID}</div>
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

  it("renders the Shadow MCP inventory management page", () => {
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

    render(<ShadowMCP />);

    expect(screen.getByRole("heading", { name: "Shadow MCP" })).toBeTruthy();
    expect(
      screen.getByText(
        "Manage project-scoped Shadow MCP server inventory and URL access rules.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Shadow MCP inventory for project-1")).toBeTruthy();
  });
});
