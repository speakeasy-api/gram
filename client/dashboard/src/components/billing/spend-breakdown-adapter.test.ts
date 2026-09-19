import { describe, expect, it } from "vitest";
import {
  adaptSpendChart,
  formatScaledUsd,
  formatSpendUsd,
  sumSelectedCost,
  validateSpendBreakdown,
  type SpendBreakdownData,
  type SpendProduct,
} from "./spend-breakdown-adapter";

const storage: SpendProduct = {
  id: "agent_session_storage",
  label: "Agent session storage",
  unit: "stokens",
  quantity: "1000000",
  rateQuantity: "1000000",
  rateUsd: "0.35",
  costUsd: "9007199254740993.001",
  buckets: [],
};

describe("metered spend precision", () => {
  it("sums selected products beyond double precision without including unselected costs", () => {
    const risk: SpendProduct = {
      ...storage,
      id: "risk_content_scans",
      costUsd: "0.009",
    };
    const egress: SpendProduct = {
      ...storage,
      id: "mcp_egress",
      costUsd: "100",
    };
    expect(
      sumSelectedCost([storage, risk, egress], new Set([storage.id, risk.id])),
    ).toBe("9007199254740993.01");
  });

  it("rounds display at the half-cent boundary without losing large whole dollars", () => {
    expect(formatSpendUsd("9007199254740993.005")).toBe(
      "$9,007,199,254,740,993.01",
    );
    expect(formatSpendUsd("1.0049999999999999999999999999")).toBe("$1.00");
    expect(formatSpendUsd("0.0000000186264514923095703125")).toBe("<$0.01");
    expect(formatScaledUsd("1005", 3)).toBe("$1.01");
  });

  it("renders malformed costs as unavailable rather than throwing or showing zero", () => {
    expect(formatSpendUsd("invalid")).toBe("—");
  });

  it("rejects malformed response costs before arithmetic", () => {
    const day = new Date("2026-09-19T00:00:00Z");
    const data: SpendBreakdownData = {
      currency: "USD",
      pricingBasis: "current_payg_list_price",
      queriedAt: day,
      window: { from: day, to: day },
      billingCycles: [],
      totalCostUsd: "0",
      products: [{ ...storage, costUsd: "0", buckets: [] }],
    };
    expect(() =>
      validateSpendBreakdown({ ...data, totalCostUsd: "invalid" }),
    ).toThrow(Error);
    data.products[0]!.costUsd = "invalid";
    expect(() => validateSpendBreakdown(data)).toThrow(Error);
    data.products[0]!.costUsd = "0";
    data.products[0]!.buckets = [
      { from: day, to: day, quantity: "0", costUsd: "invalid" },
    ];
    expect(() => validateSpendBreakdown(data)).toThrow(Error);
  });

  it.each(["2026-09-19T00:00:00Z", "2026-09-19T12:00:00Z"])(
    "keeps the current bucket and excludes future buckets at %s",
    (queriedAt) => {
      const day = (value: number) => new Date(Date.UTC(2026, 8, value));
      const data: SpendBreakdownData = {
        currency: "USD",
        pricingBasis: "current_payg_list_price",
        queriedAt: new Date(queriedAt),
        window: { from: day(18), to: day(21) },
        billingCycles: [],
        totalCostUsd: "0.3000000000000000000000000001",
        products: [
          {
            ...storage,
            buckets: [
              { from: day(18), to: day(19), quantity: "1", costUsd: "0.1" },
              {
                from: day(19),
                to: day(20),
                quantity: "2",
                costUsd: "0.2000000000000000000000000001",
              },
              { from: day(20), to: day(21), quantity: "0", costUsd: "0" },
            ],
          },
        ],
      };
      const chart = adaptSpendChart(data, new Set([storage.id]));
      expect(chart.bucketsMs).toEqual([day(18).getTime(), day(19).getTime()]);
      expect(chart.stacks[0]?.exactSeries).toEqual([
        "1000000000000000000000000000",
        "2000000000000000000000000001",
      ]);
    },
  );
});
