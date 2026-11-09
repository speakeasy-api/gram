import { describe, expect, it } from "vitest";
import { formatDaysHours } from "./format-duration";

describe("formatDaysHours", () => {
  it("reads a duration of days in days and hours", () => {
    // 5570m 34s, as the Agent Sessions list used to show it.
    expect(formatDaysHours(5570 * 60 + 34)).toBe("3d 20h");
    expect(formatDaysHours(26 * 3600 + 15 * 60)).toBe("1d 2h");
  });

  it("starts at exactly one day", () => {
    expect(formatDaysHours(86_400)).toBe("1d");
    expect(formatDaysHours(86_399)).toBeNull();
  });

  it("drops the hours on a whole number of days", () => {
    expect(formatDaysHours(2 * 86_400 + 59 * 60)).toBe("2d");
  });

  it("leaves shorter and unreadable durations to the caller", () => {
    expect(formatDaysHours(0)).toBeNull();
    expect(formatDaysHours(125 * 60 + 30)).toBeNull();
    expect(formatDaysHours(Number.NaN)).toBeNull();
  });
});
