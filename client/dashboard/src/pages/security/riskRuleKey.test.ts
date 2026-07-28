import { cleanup, render } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import RiskOverviewRulesIndex from "./RiskOverviewRulesIndex";
import { riskRuleKey } from "./riskRuleKey";

const mocks = vi.hoisted(() => ({
  useRiskOverview: vi.fn(),
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

vi.mock("@/components/DashboardTimeRangePicker", () => ({
  TimeRangePicker: () => null,
}));

vi.mock("@/components/observe/useDateRangeFilter", () => ({
  formatDateRangeLabel: () => "",
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
    riskEvents: { href: () => "/risk-events" },
  }),
}));

vi.mock("@gram/client/react-query/riskOverview.js", () => ({
  useRiskOverview: mocks.useRiskOverview,
}));

vi.mock("@speakeasy-api/moonshine", async () => {
  const { createElement } = await import("react");

  return {
    Icon: ({ name }: { name: string }) => createElement("span", null, name),
  };
});

vi.mock("react-router", async () => {
  const { createElement } = await import("react");

  return {
    Link: ({ children, to }: { children: ReactNode; to: string }) =>
      createElement("a", { href: to }, children),
    useLocation: () => ({ search: "" }),
  };
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

describe("risk rule row identity", () => {
  it.each([
    ["non-empty", "seed.history_risk"],
    ["empty", ""],
  ])("keeps %s rule IDs from different sources distinct", (_, ruleId) => {
    mocks.useRiskOverview.mockReturnValue({
      data: {
        topRules: [
          { source: "catalog", ruleId, findings: 2 },
          { source: "custom", ruleId, findings: 1 },
        ],
      },
      isLoading: false,
    });
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);

    render(RiskOverviewRulesIndex());

    const errors = consoleError.mock.calls.flat().join(" ");
    expect(errors).not.toContain("same key");
  });
});

describe("riskRuleKey", () => {
  it("distinguishes the same rule ID from different sources", () => {
    expect(riskRuleKey("catalog", "seed.history_risk")).not.toBe(
      riskRuleKey("custom", "seed.history_risk"),
    );
  });

  it("uses a stable sentinel for an empty rule ID", () => {
    expect(riskRuleKey("catalog", "")).toBe(riskRuleKey("catalog", ""));
    expect(riskRuleKey("catalog", "")).not.toBe(riskRuleKey("custom", ""));
  });

  it("distinguishes an empty rule ID from the sentinel text", () => {
    expect(riskRuleKey("catalog", "")).not.toBe(
      riskRuleKey("catalog", "__none"),
    );
  });
});
