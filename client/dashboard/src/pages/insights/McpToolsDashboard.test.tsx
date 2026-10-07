import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { McpToolsPage } from "./McpToolsDashboard";

const testState = vi.hoisted(() => ({
  flagStatus: "enabled" as
    | "enabled"
    | "disabled"
    | "loading"
    | "missing"
    | "error",
  featuresPending: false,
  logsEnabled: true,
  refetch: vi.fn(),
  navigated: [] as string[],
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: testState.flagStatus }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    isPending: testState.featuresPending,
    data: testState.featuresPending
      ? undefined
      : { logsEnabled: testState.logsEnabled },
    refetch: testState.refetch,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    dashboards: {
      detail: {
        goTo: (id: string) => {
          testState.navigated.push(`/org/projects/p/dashboards/${id}`);
        },
      },
    },
  }),
  useOrgRoutes: () => ({ logs: { href: () => "/org/settings/logs" } }),
}));
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));
vi.mock("@/components/page-layout", () => ({
  Page: { Eyebrow: () => <div data-testid="eyebrow" /> },
}));
// Today's page, the overlay and the built-in page are each tested on their
// own; here only which of them the route shows matters.
vi.mock("@/components/observe/InsightsTools", () => ({
  InsightsToolsContent: () => <div data-testid="legacy-page" />,
}));
vi.mock("@/components/EnableLoggingOverlay", () => ({
  EnableLoggingOverlay: ({ onEnabled }: { onEnabled: () => void }) => (
    <button type="button" onClick={onEnabled}>
      Enable logging
    </button>
  ),
}));
vi.mock("../explore/BuiltInDashboardPage", () => ({
  BuiltInDashboardPage: ({
    slug,
    actions,
    onOpen,
  }: {
    slug: string;
    actions?: ReactNode;
    onOpen: (dashboard: Dashboard) => void;
  }) => (
    <div data-testid="built-in" data-slug={slug}>
      {actions}
      <button
        type="button"
        onClick={() => onOpen({ id: "copy-1" } as Dashboard)}
      >
        Duplicate
      </button>
    </div>
  ),
}));

describe("McpToolsPage", () => {
  beforeEach(() => {
    testState.flagStatus = "enabled";
    testState.featuresPending = false;
    testState.logsEnabled = true;
    testState.refetch.mockClear();
    testState.navigated = [];
  });

  afterEach(() => cleanup());

  it("is the Speakeasy-built dashboard behind the flag, with Configure settings beside Duplicate", () => {
    render(<McpToolsPage />);

    const page = screen.getByTestId("built-in");
    expect(page.getAttribute("data-slug")).toBe("mcp-tools");
    expect(screen.getByTestId("eyebrow")).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: /Configure settings/ })
        .getAttribute("href"),
    ).toBe("/org/settings/logs");
    expect(screen.queryByTestId("legacy-page")).toBeNull();
  });

  it("opens the copy Duplicate makes on its Dashboards page", () => {
    render(<McpToolsPage />);

    fireEvent.click(screen.getByRole("button", { name: "Duplicate" }));
    expect(testState.navigated).toEqual(["/org/projects/p/dashboards/copy-1"]);
  });

  it("holds the place while the flag loads, showing neither page", () => {
    testState.flagStatus = "loading";
    render(<McpToolsPage />);

    expect(document.querySelector("[aria-busy='true']")).toBeTruthy();
    expect(screen.queryByTestId("built-in")).toBeNull();
    expect(screen.queryByTestId("legacy-page")).toBeNull();
  });

  it.each(["disabled", "missing", "error"] as const)(
    "is today's page when the flag is %s",
    (status) => {
      testState.flagStatus = status;
      render(<McpToolsPage />);

      expect(screen.getByTestId("legacy-page")).toBeTruthy();
      expect(screen.queryByTestId("built-in")).toBeNull();
    },
  );

  it("offers to turn logging on instead of a grid of empty cards, and reads the setting again once it is", () => {
    testState.logsEnabled = false;
    render(<McpToolsPage />);

    expect(screen.queryByTestId("built-in")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Enable logging" }));
    expect(testState.refetch).toHaveBeenCalled();
  });

  it("waits for the logging setting before drawing anything", () => {
    testState.featuresPending = true;
    render(<McpToolsPage />);

    expect(document.querySelector("[aria-busy='true']")).toBeTruthy();
    expect(screen.queryByTestId("built-in")).toBeNull();
  });
});
