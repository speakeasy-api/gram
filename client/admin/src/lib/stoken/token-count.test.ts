import { describe, expect, test } from "vitest";
import { formatTokenCount, parseTokenCount } from "./token-count";

describe("parseTokenCount", () => {
  test.each([
    ["0", 0],
    ["120000000", 120_000_000],
    ["120,000,000", 120_000_000],
    ["120M", 120_000_000],
    ["120 m", 120_000_000],
    ["1.5B", 1_500_000_000],
    [".5K", 500],
    ["1.234K", 1_234],
    ["+2T", 2_000_000_000_000],
    ["9007.199254740991T", Number.MAX_SAFE_INTEGER],
  ])("parses %s as %p tokens", (input, expected) => {
    expect(parseTokenCount(input)).toEqual({ ok: true, value: expected });
  });

  test.each([
    "",
    " ",
    "-1",
    "1.2345K",
    "1MM",
    "1,00M",
    "NaN",
    "Infinity",
    "9007.199254740992T",
  ])("rejects invalid token count %p", (input) => {
    const result = parseTokenCount(input);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toContain("You can use K, M, B, or T.");
    }
  });
});

describe("formatTokenCount", () => {
  test.each([
    [0, "0"],
    [999.4, "999"],
    [1_000, "1K"],
    [1_250, "1.3K"],
    [999_950, "1M"],
    [25_000_000, "25M"],
    [58_333_333.333, "58.3M"],
    [1_200_000_000, "1.2B"],
    [2_000_000_000_000, "2T"],
  ])("formats %p as %s", (value, expected) => {
    expect(formatTokenCount(value)).toBe(expected);
  });
});
