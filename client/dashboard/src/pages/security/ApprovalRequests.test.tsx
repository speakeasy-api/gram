import { cleanup, render, screen } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import ApprovalRequests from "./ApprovalRequests";

const mocks = vi.hoisted(() => ({
  useProject: vi.fn(),
  useOrganization: vi.fn(),
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
    let body: ReactElement | null = null;

    for (const child of Array.isArray(children) ? children : [children]) {
      if (typeof child === "object" && child && "type" in child) {
        if (child.type === Section.Body) body = child;
      }
    }

    return <div>{body}</div>;
  }
  Section.Title = () => null;
  Section.Description = () => null;
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;

  return {
    Page: Object.assign(Page, {
      Header,
      Body,
      Section,
    }),
  };
});

vi.mock("@speakeasy-api/moonshine", () => ({
  Icon: ({ name }: { name: string }) => <span>{name}</span>,
}));

vi.mock("@/components/access/ApprovalRequestsContent", () => ({
  ApprovalRequestsContent: ({
    projectID,
    projectSlug,
  }: {
    projectID: string;
    projectSlug: string;
  }) => (
    <div>
      Approval Requests Content for {projectSlug} ({projectID})
    </div>
  ),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: mocks.useProject,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: mocks.useRBAC,
}));

describe("ApprovalRequests", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("renders the org-admin content in the page section body", () => {
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

    render(<ApprovalRequests />);

    expect(
      screen.getByText("Approval Requests Content for demo (project-1)"),
    ).toBeTruthy();
  });

  it("requires org admin access", () => {
    mocks.useProject.mockReturnValue({
      id: "project-1",
      name: "Demo",
      slug: "demo",
    });
    mocks.useRBAC.mockReturnValue({
      hasAnyScope: (scopes: string[]) =>
        scopes.some(
          (scope) => scope === "project:read" || scope === "project:write",
        ),
      hasAllScopes: () => true,
      isLoading: false,
    });

    render(<ApprovalRequests />);

    expect(
      screen.queryByText("Approval Requests Content for demo (project-1)"),
    ).toBeNull();
    expect(screen.getByText("Access restricted")).toBeTruthy();
  });
});
