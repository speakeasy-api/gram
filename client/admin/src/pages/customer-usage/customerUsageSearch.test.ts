import { describe, expect, it } from "vitest";

import {
  customerUsageControls,
  customerUsageSearch,
  productsSearchValue,
} from "./customerUsageSearch";

describe("customer usage search", () => {
  it("defaults to every product, monthly, cumulative, sorted by spend", () => {
    const controls = customerUsageControls(customerUsageSearch({}));
    expect(controls).toEqual({
      q: "",
      plan: "all",
      sort: "spend",
      products: new Set([
        "agent_session_storage",
        "risk_content_scans",
        "mcp_egress",
      ]),
      interval: "monthly",
      cumulative: true,
    });
  });

  it("keeps valid values and drops invalid ones", () => {
    expect(
      customerUsageSearch({
        q: "example",
        plan: "payg",
        sort: "bogus",
        products: ["mcp_egress"],
        interval: "weekly",
        cumulative: false,
      }),
    ).toEqual({
      q: "example",
      plan: "payg",
      sort: undefined,
      products: ["mcp_egress"],
      interval: "weekly",
      cumulative: false,
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
});
