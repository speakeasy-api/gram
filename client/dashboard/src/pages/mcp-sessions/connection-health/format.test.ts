import { describe, expect, it } from "vitest";
import {
  appKeyLabel,
  formatSessionLength,
  formatWeekdayDay,
  nowPosition,
  stepResultLabel,
} from "./format";

describe("formatSessionLength", () => {
  it("never rounds hours into a day", () => {
    expect(formatSessionLength(12 * 3_600)).toBe("12 hours");
    expect(formatSessionLength(36 * 3_600)).toBe("1 day 12 hours");
    expect(formatSessionLength(14 * 86_400)).toBe("14 days");
    expect(formatSessionLength(23 * 3_600 + 31 * 60)).toBe("23 hours");
    expect(formatSessionLength(90)).toBe("2 minutes");
  });
});

describe("appKeyLabel", () => {
  it("names a kind, keeps a host and hides other", () => {
    expect(
      appKeyLabel({ family: "", name: "Unlisted app", appKey: "dcr" }),
    ).toBe("Registered itself");
    expect(
      appKeyLabel({ family: "", name: "Unlisted app", appKey: "claude.ai" }),
    ).toBe("claude.ai");
    expect(
      appKeyLabel({ family: "", name: "Unlisted app", appKey: "other" }),
    ).toBeUndefined();
  });
});

describe("nowPosition", () => {
  it("finds the bucket now is in and how far through it", () => {
    const hour = 3_600_000;
    expect(nowPosition([0, hour, 2 * hour], hour, 1.25 * hour)).toEqual({
      index: 1,
      fraction: 0.25,
    });
    expect(nowPosition([0, hour], hour, 3 * hour)).toBeNull();
  });
});

describe("stepResultLabel", () => {
  it("words known results and capitalises unknown ones", () => {
    expect(stepResultLabel("refreshed")).toBe("Renewed");
    expect(stepResultLabel("token_swapped")).toBe("Token swapped");
  });
});

describe("formatWeekdayDay", () => {
  it("puts the weekday first", () => {
    expect(formatWeekdayDay(new Date(2026, 10, 11, 12).getTime())).toMatch(
      /^\S+ 11$/,
    );
  });
});
