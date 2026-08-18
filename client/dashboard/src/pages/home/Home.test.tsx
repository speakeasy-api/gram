import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
const searchParams = vi.hoisted(() => new URLSearchParams());

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
  useRBAC: () => ({ hasAnyScope: () => true, isLoading: false }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ mcp: { href: () => "/mcp" } }),
}));
vi.mock("react-router", () => ({
  Navigate: () => null,
  useSearchParams: () => [searchParams],
}));

import Home from "./Home.tsx";

afterEach(() => {
  cleanup();
  searchParams.delete("showGuide");
});

describe("Home", () => {
  it("takes the space with the guide when showGuide is present", () => {
    searchParams.set("showGuide", "");
    render(<Home />);
    expect(screen.getByTestId("project-guide")).toBeTruthy();
    expect(screen.queryByTestId("chat-landing")).toBeNull();
    expect(screen.queryByTestId("project-dashboard")).toBeNull();
  });

  it("keeps the assistant and dashboard when showGuide is absent", () => {
    render(<Home />);
    expect(screen.getByTestId("chat-landing")).toBeTruthy();
    expect(screen.getByTestId("project-dashboard")).toBeTruthy();
    expect(screen.queryByTestId("project-guide")).toBeNull();
  });
});
