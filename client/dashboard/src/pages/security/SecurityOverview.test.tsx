import { cleanup, render } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import SecurityOverview from "./SecurityOverview";

const mocks = vi.hoisted(() => ({
  useRiskOverview: vi.fn(),
}));

vi.mock("@/components/chart/ChartCard", () => ({
  ChartCard: () => null,
}));

vi.mock("@/components/chart/MetricCard", () => ({
  MetricCard: () => null,
}));

vi.mock("@/components/insights-dock", () => ({
  InsightsConfig: () => null,
}));

vi.mock("@/components/page-layout", async () => {
  const { createElement } = await import("react");

  function Container({ children }: { children?: ReactNode }) {
    return createElement("div", null, children);
  }

  const Header = Object.assign(Container, { Breadcrumbs: () => null });
  const Section = Object.assign(Container, {
    Title: Container,
    Description: Container,
    CTA: Container,
    Body: Container,
  });

  return {
    Page: Object.assign(Container, {
      Header,
      Body: Container,
      Section,
    }),
  };
});

vi.mock("@/components/require-scope", async () => {
  const { createElement, Fragment } = await import("react");

  return {
    RequireScope: ({ children }: { children: ReactNode }) =>
      createElement(Fragment, null, children),
  };
});

vi.mock("@/components/ui/dashboard-card", async () => {
  const { createElement } = await import("react");

  return {
    DashboardCard: ({ children }: { children: ReactNode }) =>
      createElement("div", null, children),
  };
});

vi.mock("@/components/ui/skeleton", () => ({
  Skeleton: () => null,
}));

vi.mock("@/components/DashboardTimeRangePicker", () => ({
  TimeRangePicker: () => null,
}));

vi.mock("@/components/observe/useDateRangeFilter", () => ({
  formatDateRangeLabel: () => "the selected range",
  useDateRangeFilter: () => ({
    dateRange: "7d",
    customRange: null,
    customRangeLabel: "",
    from: undefined,
    to: undefined,
    setDateRangeParam: vi.fn(),
    setCustomRangeParam: vi.fn(),
    clearCustomRange: vi.fn(),
  }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    agentSessions: { href: () => "/sessions" },
    policyCenter: { href: () => "/policies" },
    riskEvents: { href: () => "/risk-events" },
    riskOverview: {
      categoriesIndex: { href: () => "/risk/categories" },
      rulesIndex: { href: () => "/risk/rules" },
      usersIndex: { href: () => "/risk/users" },
    },
  }),
}));

vi.mock("@gram/client/react-query/riskOverview.js", () => ({
  useRiskOverview: mocks.useRiskOverview,
}));

vi.mock("@speakeasy-api/moonshine", async () => {
  const { createElement } = await import("react");

  function Container({ children }: { children?: ReactNode }) {
    return createElement("span", null, children);
  }

  return {
    Button: Object.assign(Container, {
      LeftIcon: Container,
      RightIcon: Container,
      Text: Container,
    }),
    Icon: ({ name }: { name: string }) => createElement("span", null, name),
  };
});

vi.mock("chart.js", () => ({
  CategoryScale: {},
  Chart: { register: vi.fn() },
  Filler: {},
  Legend: {},
  LinearScale: {},
  LineElement: {},
  PointElement: {},
  Tooltip: {},
}));

vi.mock("chartjs-plugin-zoom", () => ({ default: {} }));
vi.mock("react-chartjs-2", () => ({ Line: () => null }));

vi.mock("react-router", async () => {
  const { createElement } = await import("react");

  return {
    Link: ({ children, to }: { children: ReactNode; to: string }) =>
      createElement("a", { href: to }, children),
    Outlet: () => null,
    useLocation: () => ({ search: "" }),
  };
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

describe("SecurityOverview rule identity", () => {
  it.each([
    ["non-empty", "seed.history_risk"],
    ["empty", ""],
  ])("keeps %s rule IDs from different sources distinct", (_, ruleId) => {
    mocks.useRiskOverview.mockReturnValue({
      data: {
        activePolicies: 1,
        findings: 3,
        flaggedSessions: 1,
        from: new Date().toISOString(),
        messagesScanned: 10,
        timeSeriesFindings: [],
        to: new Date().toISOString(),
        topCategories: [],
        topRules: [
          { source: "catalog", ruleId, findings: 2 },
          { source: "custom", ruleId, findings: 1 },
        ],
        topUsers: [],
      },
      error: null,
      isLoading: false,
    });
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);

    render(<SecurityOverview />);

    const errors = consoleError.mock.calls.flat().join(" ");
    expect(errors).not.toContain("same key");
  });
});
