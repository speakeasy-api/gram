import { describe, expect, it } from "vitest";
import { formatExactUsd } from "./payg-billing-estimate";

describe("formatExactUsd", () => {
  it.each([
    ["0", "$0.00"],
    ["12", "$12.00"],
    ["12.5", "$12.50"],
    ["12.50", "$12.50"],
    // Trailing zeros below cents are noise; the cents themselves are not.
    ["12.5000", "$12.50"],
    ["0.10", "$0.10"],
    ["1234.56", "$1,234.56"],
    ["1234567.891", "$1,234,567.891"],
    // A per-token unit price carries more precision than cents, and truncating
    // it to "$0.00" would misprice the whole line.
    ["0.00000015", "$0.00000015"],
    ["0.000000000001", "$0.000000000001"],
    ["+7.25", "$7.25"],
    ["-3.5", "-$3.50"],
    ["007.5", "$7.50"],
    ["0000", "$0.00"],
    // A signed zero is zero — "-$0.00" reads as a refund that isn't one.
    ["-0.00", "$0.00"],
    ["-0.000000", "$0.00"],
    ["3.", "$3.00"],
  ])("formats %s exactly as %s", (input, expected) => {
    expect(formatExactUsd(input)).toBe(expected);
  });

  // An amount larger than a double holds must survive intact: it is grouped
  // from its own digits, never parsed.
  it("keeps an amount beyond double precision exact", () => {
    expect(formatExactUsd("9007199254740993.01")).toBe(
      "$9,007,199,254,740,993.01",
    );
  });

  it.each([
    ["", "empty"],
    ["  ", "blank"],
    ["1e3", "exponent"],
    ["$12.00", "already formatted"],
    ["12,00", "comma"],
    ["abc", "letters"],
    ["1.2.3", "two points"],
    [".5", "no whole part"],
  ])("refuses %s (%s)", (input) => {
    expect(formatExactUsd(input)).toBeNull();
  });

  it.each([undefined, null])("refuses a %s amount", (input) => {
    expect(formatExactUsd(input)).toBeNull();
  });
});
