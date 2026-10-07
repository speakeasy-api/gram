import { describe, expect, it } from "vitest";

import {
  customerUsageControls,
  customerUsageSearch,
  plansSearchValue,
  productsSearchValue,
} from "./customerUsageSearch";

describe("customer usage search", () => {
  it("defaults to every product, monthly, per-period (not cumulative), sorted by spend", () => {
    const controls = customerUsageControls(customerUsageSearch({}));
    expect(controls).toEqual({
      q: "",
      plans: new Set(),
      sort: "spend",
      products: new Set([
        "agent_session_storage",
        "risk_content_scans",
        "mcp_egress",
      ]),
      interval: "monthly",
      cumulative: false,
    });
  });

  it("keeps valid values and drops invalid ones", () => {
    expect(
      customerUsageSearch({
        q: "example",
        plans: ["payg", "enterprise"],
        sort: "bogus",
        products: ["mcp_egress"],
        interval: "weekly",
        cumulative: true,
      }),
    ).toEqual({
      q: "example",
      plans: ["payg", "enterprise"],
      sort: undefined,
      products: ["mcp_egress"],
      interval: "weekly",
      cumulative: true,
    });
  });

  it("keeps an empty product selection distinct from all products", () => {
    expect(
      customerUsageControls(customerUsageSearch({ products: [] })).products,
    ).toEqual(new Set());
  });

  it("leaves the full product selection out of the URL", () => {
    expect(
      productsSearchValue(
        new Set(["mcp_egress", "agent_session_storage", "risk_content_scans"]),
      ),
    ).toBeUndefined();
    expect(
      productsSearchValue(new Set(["mcp_egress", "agent_session_storage"])),
    ).toEqual(["agent_session_storage", "mcp_egress"]);
  });

  it("leaves no plan and every plan out of the URL", () => {
    expect(plansSearchValue(new Set())).toBeUndefined();
    expect(
      plansSearchValue(new Set(["pro", "payg", "enterprise"])),
    ).toBeUndefined();
    expect(plansSearchValue(new Set(["payg", "enterprise"]))).toEqual([
      "enterprise",
      "payg",
    ]);
  });

  it("drops an unknown plan list", () => {
    expect(customerUsageSearch({ plans: ["gold"] }).plans).toBeUndefined();
  });
});
