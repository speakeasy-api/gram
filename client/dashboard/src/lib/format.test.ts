import { describe, expect, it } from "vitest";
import { formatCompact, pluralize } from "./format";

describe("formatCompact", () => {
  it("passes through small numbers unchanged", () => {
    expect(formatCompact(0)).toBe("0");
    expect(formatCompact(42)).toBe("42");
    expect(formatCompact(999)).toBe("999");
  });

  it("abbreviates thousands with K", () => {
    expect(formatCompact(1_000)).toBe("1K");
    expect(formatCompact(1_500)).toBe("1.5K");
    expect(formatCompact(999_999)).toBe("1M"); // Intl rounds up
  });

  it("abbreviates millions with M", () => {
    expect(formatCompact(1_000_000)).toBe("1M");
    expect(formatCompact(2_300_000)).toBe("2.3M");
  });

  it("abbreviates billions with B", () => {
    expect(formatCompact(1_000_000_000)).toBe("1B");
    expect(formatCompact(1_100_000_000)).toBe("1.1B");
  });

  it("drops trailing zeros on round values", () => {
    expect(formatCompact(1_000)).toBe("1K");
    expect(formatCompact(10_000)).toBe("10K");
    expect(formatCompact(1_000_000)).toBe("1M");
  });

  it("handles negative numbers", () => {
    expect(formatCompact(-1_500)).toBe("-1.5K");
    expect(formatCompact(-1_000_000)).toBe("-1M");
  });
});

describe("pluralize", () => {
  it("pluralizes the noun except when the count is one", () => {
    expect(pluralize(1, "selected server")).toBe("1 selected server");
    expect(pluralize(2, "server")).toBe("2 servers");
    expect(pluralize(0, "server")).toBe("0 servers");
  });

  it("follows the regular -ies and -es rules", () => {
    expect(pluralize(2, "policy")).toBe("2 policies");
    expect(pluralize(2, "status")).toBe("2 statuses");
    expect(pluralize(2, "key")).toBe("2 keys");
  });
});
