import { describe, expect, it } from "vitest";

import { formatBillingDate } from "./billingState";

describe("formatBillingDate", () => {
  it("returns null for an invalid Date object", () => {
    expect(formatBillingDate(new Date("invalid"))).toBeNull();
  });
});
