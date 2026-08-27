import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
vi.mock("@/components/project/ProjectDashboard", () => ({
  ProjectDashboard: () => <div data-testid="project-dashboard" />,
}));
vi.mock("@/pages/chat/Chat", () => ({
  ChatLanding: () => <div data-testid="chat-landing" />,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ mcp: { href: () => "/org/projects/project/mcp" } }),
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
      Body: ({ children }: { children: ReactNode }) => <>{children}</>,
    },
  );
  return { Page };
});
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAnyScope: () => true,
    hasAnyScopeInProject: () => true,
    isLoading: false,
  }),
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
  });
});
