import { describe, expect, it } from "vitest";
import { formatBillingCurrency, formatBillingQuantity } from "./billing-format";

describe("billing formatting", () => {
  it("formats fractional quantities with explicit units", () => {
    expect(formatBillingQuantity(2.5, "chat credits")).toBe("2.5 chat credits");
    expect(formatBillingQuantity(1, "servers")).toBe("1 server");
  });

  it("formats monetary values as USD with cents", () => {
    expect(formatBillingCurrency(29)).toBe("$29.00");
    expect(formatBillingCurrency(11.5)).toBe("$11.50");
  });
});
