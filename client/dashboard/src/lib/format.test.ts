import { describe, expect, it } from "vitest";
import { formatCompact } from "./format";

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
