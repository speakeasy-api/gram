import { describe, expect, it } from "vitest";

import { formatFunctionTimeout, parseFunctionTimeout } from "./functionTimeout";

describe("parseFunctionTimeout", () => {
  it.each(["30", "60", " 300 ", "600"])("accepts %j", (value) => {
    expect(parseFunctionTimeout(value)).toEqual({
      seconds: Number(value.trim()),
      problem: null,
    });
  });

  it.each(["", "29", "601", "0", "-30", "45.5", "1e2", "abc", "60s"])(
    "rejects %j",
    (value) => {
      const parsed = parseFunctionTimeout(value);
      expect(parsed.seconds).toBeNull();
      expect(parsed.problem).toBe(
        "Enter a whole number of seconds from 30 to 600.",
      );
    },
  );
});

describe("formatFunctionTimeout", () => {
  it.each([
    [45, "45 seconds"],
    [60, "1 minute"],
    [90, "1 minute 30 seconds"],
    [61, "1 minute 1 second"],
    [600, "10 minutes"],
  ])("formats %d as %j", (seconds, text) => {
    expect(formatFunctionTimeout(seconds)).toBe(text);
  });
});
