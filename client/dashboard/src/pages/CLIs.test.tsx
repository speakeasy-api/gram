import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import CLIs from "./CLIs";

const productFeatures = vi.hoisted(() => ({ skillsEnabled: false }));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { skillsEnabled: productFeatures.skillsEnabled },
  }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
  }: {
    children: ReactNode;
    scope: string;
  }) => (
    <div data-testid="scope-gate" data-scope={scope}>
      {children}
    </div>
  ),
}));

vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <div>{children}</div>;
  }

  function Header({ children }: { children?: ReactNode }) {
    return <header>{children}</header>;
  }

  function Section({ children }: { children: ReactNode }) {
    return <section>{children}</section>;
  }

  return {
    Page: Object.assign(Page, {
      Header: Object.assign(Header, {
        Breadcrumbs: () => <nav>Breadcrumbs</nav>,
      }),
      Body: ({ children }: { children: ReactNode }) => <main>{children}</main>,
      Section: Object.assign(Section, {
        Title: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
        Description: ({ children }: { children: ReactNode }) => (
          <p>{children}</p>
        ),
        Body: ({ children }: { children: ReactNode }) => <div>{children}</div>,
      }),
    }),
  };
});

vi.mock("@speakeasy-api/moonshine", () => ({
  Icon: () => <span data-testid="skills-icon" />,
}));

afterEach(cleanup);

describe("CLIs", () => {
  it("preserves the Coming Soon page and project gate when Skills is disabled", () => {
    productFeatures.skillsEnabled = false;

    render(<CLIs />);

    expect(screen.getByTestId("scope-gate").getAttribute("data-scope")).toBe(
      "project:read",
    );
    expect(screen.getByText("Coming Soon")).toBeTruthy();
    expect(screen.getByText("No skills yet")).toBeTruthy();
  });

  it("renders the enabled scaffold behind the Skills read gate", () => {
    productFeatures.skillsEnabled = true;

    render(<CLIs />);

    expect(screen.getByTestId("scope-gate").getAttribute("data-scope")).toBe(
      "skill:read",
    );
    expect(screen.queryByText("Coming Soon")).toBeNull();
    expect(screen.getByText("No skills yet")).toBeTruthy();
  });
});
