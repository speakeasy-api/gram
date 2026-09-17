import { describe, expect, it } from "vitest";

import {
  billingUsageSearch,
  exclusiveEnd,
  usageRangeError,
} from "./billingUsageSearch";

describe("billing usage ranges", () => {
  it("validates the exclusive boundary against clamped calendar months", () => {
    expect(usageRangeError("2026-01-31", "2026-04-29")).toBeUndefined();
    expect(usageRangeError("2026-01-31", "2026-04-30")).toBeDefined();
    expect(exclusiveEnd("2026-04-29")).toBe("2026-04-30T00:00:00.000Z");
  });

  it("rejects impossible calendar dates and reversed intervals", () => {
    expect(usageRangeError("2026-02-29", "2026-03-01")).toBeDefined();
    expect(usageRangeError("2026-09-10", "2026-09-09")).toBeDefined();
    expect(usageRangeError("2026-09-10", "2026-09-10")).toBeUndefined();
  });

  it("drops invalid or unpaired URL bounds without discarding valid product filters", () => {
    expect(
      billingUsageSearch({
        product: "mcp_bandwidth",
        interval: "weekly",
        from: "2026-02-30",
        to: "2026-03-01",
      }),
    ).toEqual({
      product: "mcp_bandwidth",
      interval: "weekly",
      from: undefined,
      to: undefined,
    });
    expect(billingUsageSearch({ from: "2026-03-01" }).from).toBeUndefined();
  });
});
