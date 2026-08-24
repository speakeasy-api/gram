import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
vi.mock("@/components/project/ProjectDashboard", () => ({
  ProjectDashboard: () => <div data-testid="project-dashboard" />,
}));
vi.mock("@/pages/chat/Chat", () => ({
  ChatLanding: () => <div data-testid="chat-landing" />,
}));
vi.mock("@/components/insights-context", () => ({
  useHideInsightsDock: () => undefined,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-layout", () => {
  const Page = Object.assign(
    ({ children }: { children: ReactNode }) => <>{children}</>,
    {
      Header: Object.assign(
        ({ children }: { children?: ReactNode }) => <>{children}</>,
        { Breadcrumbs: () => null },
      ),
      Body: ({
        children,
        fullWidth,
        fullHeight,
        noPadding,
      }: {
        children: ReactNode;
        fullWidth?: boolean;
        fullHeight?: boolean;
        noPadding?: boolean;
      }) => (
        <div
          data-testid="page-body"
          data-full-width={fullWidth}
          data-full-height={fullHeight}
          data-no-padding={noPadding}
        >
          {children}
        </div>
      ),
    },
  );
  return { Page };
});
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true, isLoading: false }),
}));
import Home from "./Home.tsx";

afterEach(() => {
  cleanup();
});

describe("Home", () => {
  it("always renders the normal project overview", () => {
    render(<Home />);
    expect(screen.getByTestId("chat-landing")).toBeTruthy();
    expect(screen.getByTestId("project-dashboard")).toBeTruthy();
    expect(screen.getByTestId("page-body").dataset.fullWidth).toBeUndefined();
    expect(screen.getByTestId("page-body").dataset.fullHeight).toBeUndefined();
    expect(screen.getByTestId("page-body").dataset.noPadding).toBeUndefined();
  });
});
