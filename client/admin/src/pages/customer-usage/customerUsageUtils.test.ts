import { describe, expect, it } from "vitest";

import type { SpendProductID } from "@/pages/organization/spendBreakdownUtils";
import { ALL_PRODUCT_IDS, type CustomerPlan } from "./customerUsageSearch";
import {
  compareCustomers,
  customerUsageView,
  planFilterLabel,
  summarizeCustomer,
  type CustomerUsage,
} from "./customerUsageUtils";

const ALL = new Set<SpendProductID>(ALL_PRODUCT_IDS);
const cycle = {
  from: new Date(Date.UTC(2026, 8, 25)),
  to: new Date(Date.UTC(2026, 9, 25)),
};

function customer({
  name,
  accountType = "enterprise",
  costs = ["0", "0", "0"],
  previous,
  error,
}: {
  name: string;
  accountType?: CustomerUsage["accountType"];
  costs?: [string, string, string];
  previous?: [string, string, string];
  error?: string;
}): CustomerUsage {
  return {
    organizationId: `org_${name}`,
    name,
    slug: name.toLowerCase(),
    accountType,
    trialState: "none",
    currentCycle: cycle,
    window: cycle,
    error,
    products: error
      ? []
      : ALL_PRODUCT_IDS.map((id, index) => ({
          id,
          label: id,
          unit: id === "mcp_egress" ? "bytes" : "stokens",
          quantity: "0",
          rateQuantity: "1",
          rateUsd: "1",
          costUsd: costs[index] ?? "0",
          buckets: [],
        })),
    previousPeriod: previous
      ? {
          from: new Date(Date.UTC(2026, 7, 25)),
          to: new Date(Date.UTC(2026, 8, 7)),
        }
      : undefined,
    previousPeriodCosts: previous
      ? ALL_PRODUCT_IDS.map((productId, index) => ({
          productId,
          costUsd: previous[index] ?? "0",
        }))
      : [],
  };
}

describe("summarizeCustomer", () => {
  it("sums only the selected products for spend and change", () => {
    const summary = summarizeCustomer(
      customer({
        name: "A",
        costs: ["10", "5", "1.5"],
        previous: ["4", "100", "0.5"],
      }),
      new Set<SpendProductID>(["agent_session_storage", "mcp_egress"]),
    );
    expect(summary.cycleCostUsd).toBe("11.5");
    expect(summary.change).toEqual({
      kind: "change",
      changeUsd: "7",
      percent: 155.55555555555557,
    });
  });

  it("reports a decrease as a negative change", () => {
    const summary = summarizeCustomer(
      customer({
        name: "A",
        costs: ["5", "0", "0"],
        previous: ["10", "0", "0"],
      }),
      ALL,
    );
    expect(summary.change).toEqual({
      kind: "change",
      changeUsd: "-5",
      percent: -50,
    });
  });

  it("has no change without a previous cycle", () => {
    expect(
      summarizeCustomer(customer({ name: "A", costs: ["5", "0", "0"] }), ALL)
        .change,
    ).toEqual({ kind: "none" });
  });

  it("reads as new when nothing was spent in the comparison period", () => {
    expect(
      summarizeCustomer(
        customer({
          name: "A",
          costs: ["5", "0", "0"],
          previous: ["0", "0", "0"],
        }),
        ALL,
      ).change,
    ).toEqual({ kind: "new", changeUsd: "5" });
  });

  it("is a flat zero change when nothing was spent in either period", () => {
    expect(
      summarizeCustomer(customer({ name: "A", previous: ["0", "0", "0"] }), ALL)
        .change,
    ).toEqual({ kind: "change", changeUsd: "0", percent: 0 });
  });

  it("is zero spend with no products selected", () => {
    expect(
      summarizeCustomer(
        customer({ name: "A", costs: ["5", "1", "1"] }),
        new Set(),
      ).cycleCostUsd,
    ).toBe("0");
  });
});

describe("compareCustomers", () => {
  const names = (
    sort: Parameters<typeof compareCustomers>[0],
    customers: CustomerUsage[],
  ) =>
    customers
      .map((item) => summarizeCustomer(item, ALL))
      .sort(compareCustomers(sort))
      .map((summary) => summary.customer.name);

  it("sorts by spend high to low", () => {
    expect(
      names("spend", [
        customer({ name: "Low", costs: ["1", "0", "0"] }),
        customer({ name: "High", costs: ["100", "0", "0"] }),
        customer({ name: "Mid", costs: ["10", "0", "0"] }),
      ]),
    ).toEqual(["High", "Mid", "Low"]);
  });

  it("sorts by absolute dollar change, either direction, with no-comparison customers last", () => {
    expect(
      names("change", [
        customer({
          name: "SmallUp",
          costs: ["2", "0", "0"],
          previous: ["1", "0", "0"],
        }),
        customer({
          name: "BigDown",
          costs: ["0", "0", "0"],
          previous: ["50", "0", "0"],
        }),
        customer({ name: "NoPrevious", costs: ["999", "0", "0"] }),
        customer({
          name: "MidUp",
          costs: ["20", "0", "0"],
          previous: ["0", "0", "0"],
        }),
      ]),
    ).toEqual(["BigDown", "MidUp", "SmallUp", "NoPrevious"]);
  });

  it("sorts by name case-insensitively", () => {
    expect(
      names("name", [
        customer({ name: "beta" }),
        customer({ name: "Alpha" }),
        customer({ name: "Gamma" }),
      ]),
    ).toEqual(["Alpha", "beta", "Gamma"]);
  });

  it("sorts by plan type, then spend", () => {
    expect(
      names("plan", [
        customer({
          name: "Payg",
          accountType: "payg",
          costs: ["100", "0", "0"],
        }),
        customer({ name: "EntLow", costs: ["1", "0", "0"] }),
        customer({ name: "Pro", accountType: "pro" }),
        customer({ name: "EntHigh", costs: ["50", "0", "0"] }),
      ]),
    ).toEqual(["EntHigh", "EntLow", "Pro", "Payg"]);
  });
});

describe("customerUsageView", () => {
  const customers = [
    customer({ name: "Example Co", costs: ["10", "0", "0"] }),
    customer({ name: "Idle Inc", accountType: "payg" }),
    customer({ name: "Broken Ltd", error: "Could not read." }),
    customer({
      name: "Storage Only",
      accountType: "pro",
      costs: ["0", "0", "3"],
    }),
  ];
  const controls = {
    q: "",
    plans: new Set<CustomerPlan>(),
    sort: "spend" as const,
    products: ALL,
  };

  it("puts customers with no usage this cycle in their own section and keeps errors in the main grid", () => {
    const view = customerUsageView(customers, controls);
    expect(view.active.map((summary) => summary.customer.name)).toEqual([
      "Example Co",
      "Storage Only",
      "Broken Ltd",
    ]);
    expect(view.idle.map((summary) => summary.customer.name)).toEqual([
      "Idle Inc",
    ]);
  });

  it("moves a customer to no usage when its only spending product is unchecked", () => {
    const view = customerUsageView(customers, {
      ...controls,
      products: new Set<SpendProductID>([
        "agent_session_storage",
        "risk_content_scans",
      ]),
    });
    expect(view.idle.map((summary) => summary.customer.name)).toEqual([
      "Idle Inc",
      "Storage Only",
    ]);
  });

  it("filters by name, case-insensitively, and by plan in both sections", () => {
    expect(
      customerUsageView(customers, { ...controls, q: "EXAMPLE" }).active.map(
        (summary) => summary.customer.name,
      ),
    ).toEqual(["Example Co"]);
    const payg = customerUsageView(customers, {
      ...controls,
      plans: new Set<CustomerPlan>(["payg"]),
    });
    expect(payg.active).toEqual([]);
    expect(payg.idle.map((summary) => summary.customer.name)).toEqual([
      "Idle Inc",
    ]);
  });

  it("shows every selected plan when several are picked", () => {
    const view = customerUsageView(customers, {
      ...controls,
      plans: new Set<CustomerPlan>(["pro", "payg"]),
    });
    expect(view.active.map((summary) => summary.customer.name)).toEqual([
      "Storage Only",
    ]);
    expect(view.idle.map((summary) => summary.customer.name)).toEqual([
      "Idle Inc",
    ]);
  });
});

describe("planFilterLabel", () => {
  it("reads All plans for none or every plan, otherwise names the selection", () => {
    expect(planFilterLabel(new Set())).toBe("All plans");
    expect(planFilterLabel(new Set(["payg", "pro", "enterprise"]))).toBe(
      "All plans",
    );
    expect(planFilterLabel(new Set(["payg", "enterprise"]))).toBe(
      "Enterprise, PAYG",
    );
  });
});

describe("pinned customers", () => {
  const customers = [
    customer({ name: "High", costs: ["100", "0", "0"] }),
    customer({ name: "Low", costs: ["1", "0", "0"] }),
    customer({ name: "Idle", accountType: "payg" }),
  ];
  const controls = {
    q: "",
    plans: new Set<CustomerPlan>(),
    sort: "spend" as const,
    products: ALL,
  };

  it("lifts pinned customers out of the other sections, including one with no usage", () => {
    const view = customerUsageView(
      customers,
      controls,
      new Set(["org_Low", "org_Idle"]),
    );
    expect(view.pinned.map((summary) => summary.customer.name)).toEqual([
      "Low",
      "Idle",
    ]);
    expect(view.active.map((summary) => summary.customer.name)).toEqual([
      "High",
    ]);
    expect(view.idle).toEqual([]);
  });

  it("still applies search and plan filters to pinned customers", () => {
    const view = customerUsageView(
      customers,
      { ...controls, plans: new Set<CustomerPlan>(["enterprise"]) },
      new Set(["org_Idle"]),
    );
    expect(view.pinned).toEqual([]);
  });
});
