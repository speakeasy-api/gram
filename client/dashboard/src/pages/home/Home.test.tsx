import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
const projectGuideStatus = vi.hoisted(() => ({ current: "dashboard" }));

vi.mock("@/components/project-guide/ProjectGuide", () => ({
  ProjectGuide: () => <div data-testid="project-guide" />,
}));
vi.mock("@/components/project/ProjectDashboard", () => ({
  ProjectDashboard: () => <div data-testid="project-dashboard" />,
}));
vi.mock("@/pages/chat/Chat", () => ({
  ChatLanding: () => <div data-testid="chat-landing" />,
}));
vi.mock("@/components/insights-context", () => ({
  useHideInsightsDock: () => undefined,
}));
vi.mock("@/hooks/useProjectGuide", () => ({
  useProjectGuide: () => ({ status: projectGuideStatus.current }),
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
vi.mock("@/routes", () => ({
  useRoutes: () => ({ mcp: { href: () => "/mcp" } }),
}));
vi.mock("react-router", () => ({ Navigate: () => null }));

import Home from "./Home.tsx";

afterEach(() => {
  cleanup();
  projectGuideStatus.current = "dashboard";
});

describe("Home", () => {
  it("takes the space with the guide when the project is empty", () => {
    projectGuideStatus.current = "guide";
    render(<Home />);
    expect(screen.getByTestId("project-guide")).toBeTruthy();
    expect(screen.queryByTestId("chat-landing")).toBeNull();
    expect(screen.queryByTestId("project-dashboard")).toBeNull();
    expect(screen.getByTestId("page-body").dataset).toMatchObject({
      fullWidth: "true",
      fullHeight: "true",
      noPadding: "true",
    });
  });

  it("keeps the assistant and dashboard when the project has data", () => {
    render(<Home />);
    expect(screen.getByTestId("chat-landing")).toBeTruthy();
    expect(screen.getByTestId("project-dashboard")).toBeTruthy();
    expect(screen.queryByTestId("project-guide")).toBeNull();
    expect(screen.getByTestId("page-body").dataset.fullWidth).toBeUndefined();
    expect(screen.getByTestId("page-body").dataset.fullHeight).toBeUndefined();
    expect(screen.getByTestId("page-body").dataset.noPadding).toBeUndefined();
  });

  it("waits for the zero-data checks before choosing a surface", () => {
    projectGuideStatus.current = "pending";
    render(<Home />);
    expect(screen.getByTestId("project-guide-pending")).toBeTruthy();
    expect(screen.queryByTestId("project-guide")).toBeNull();
    expect(screen.queryByTestId("project-dashboard")).toBeNull();
  });
});
