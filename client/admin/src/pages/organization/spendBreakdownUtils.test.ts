import { describe, expect, it } from "vitest";

import {
  formatSpendUsd,
  sumSpendCosts,
  spendChartData,
  type AdminSpendBreakdown,
  type SpendProduct,
} from "./spendBreakdownUtils";

function day(value: number): Date {
  return new Date(Date.UTC(2026, 8, value));
}

function product(overrides: Partial<SpendProduct> = {}): SpendProduct {
  return {
    id: "agent_session_storage",
    label: "Agent session storage",
    unit: "stokens",
    quantity: "2",
    rateQuantity: "1",
    rateUsd: "0.000000000000000001",
    costUsd: "0.000000000000000002",
    buckets: [
      {
        from: day(1),
        to: day(2),
        quantity: "1",
        costUsd: "0.000000000000000001",
      },
      {
        from: day(2),
        to: day(3),
        quantity: "1",
        costUsd: "0.000000000000000001",
      },
      {
        from: day(3),
        to: day(4),
        quantity: "0",
        costUsd: "999999999999999999.99",
      },
    ],
    ...overrides,
  };
}

describe("spend currency display", () => {
  it("rounds the display to cents after summing exact amounts", () => {
    expect(formatSpendUsd("70.161180542578125")).toBe("$70.16");
    expect(formatSpendUsd(sumSpendCosts(["0.004", "0.004"]))).toBe("$0.01");
  });

  it("rounds half cents with dollar carry without losing large-integer precision", () => {
    expect(formatSpendUsd("9007199254740992.995")).toBe(
      "$9,007,199,254,740,993.00",
    );
  });

  it("distinguishes sub-cent amounts from true zero without exposing extra decimals", () => {
    expect(formatSpendUsd("0.004")).toBe("<$0.01");
    expect(formatSpendUsd("-0.004")).toBe(">-$0.01");
    expect(formatSpendUsd("0.005")).toBe("$0.01");
    expect(formatSpendUsd("-0.000")).toBe("$0.00");
  });
});

describe("spend chart arithmetic", () => {
  it("aggregates exact sub-cent decimals before chart scaling and excludes future buckets", () => {
    const data: AdminSpendBreakdown = {
      billingCycles: [],
      currency: "USD",
      pricingBasis: "current_payg_list_price",
      queriedAt: new Date("2026-09-02T12:00:00Z"),
      totalCostUsd: "0.000000000000000002",
      window: { from: day(1), to: day(4) },
      products: [product()],
    };

    const chart = spendChartData(
      data,
      new Set<SpendProduct["id"]>(["agent_session_storage"]),
      "weekly",
      false,
    );

    expect(chart.scale).toBe(18);
    expect(chart.maximum).toBe(2);
    expect(chart.points).toHaveLength(1);
    expect(chart.points[0]?.exactCostUsd).toBe("0.000000000000000002");
    expect(chart.points[0]?.scaledCost).toBe(2);
    expect(chart.points[0]?.inProgress).toBe(true);

    const cumulative = spendChartData(
      data,
      new Set<SpendProduct["id"]>(["agent_session_storage"]),
      "daily",
      true,
    );
    expect(
      cumulative.points.map(({ exactCostUsd, inProgress }) => ({
        exactCostUsd,
        inProgress,
      })),
    ).toEqual([
      { exactCostUsd: "0.000000000000000001", inProgress: false },
      { exactCostUsd: "0.000000000000000002", inProgress: true },
    ]);
  });
});
