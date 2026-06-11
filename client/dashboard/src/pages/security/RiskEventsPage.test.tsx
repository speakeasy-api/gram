import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import RiskEventsPage from "./RiskEventsPage";

vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <div data-testid="page">{children}</div>;
  }

  function Header({ children }: { children?: ReactNode }) {
    return <div data-testid="page-header">{children}</div>;
  }
  Header.Breadcrumbs = ({ fullWidth }: { fullWidth?: boolean }) => (
    <nav data-full-width={fullWidth ? "true" : "false"}>Breadcrumbs</nav>
  );

  function Body({ children }: { children: ReactNode }) {
    return <main data-testid="page-body">{children}</main>;
  }

  return {
    Page: Object.assign(Page, {
      Header,
      Body,
    }),
  };
});

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("./RiskEvents", () => ({
  default: () => <div>Risk Events Content</div>,
}));

afterEach(cleanup);

describe("RiskEventsPage", () => {
  it("renders the standalone risk events route with breadcrumbs", () => {
    render(<RiskEventsPage />);

    expect(screen.getByText("Breadcrumbs")).toBeTruthy();
    expect(
      screen.getByText("Breadcrumbs").getAttribute("data-full-width"),
    ).toBe("true");
    expect(screen.getByText("Risk Events Content")).toBeTruthy();
  });
});
