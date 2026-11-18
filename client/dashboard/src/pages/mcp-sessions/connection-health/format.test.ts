import { describe, expect, it } from "vitest";
import { appKeyLabel, formatSessionLength } from "./format";

describe("formatSessionLength", () => {
  it("never rounds hours into a day", () => {
    expect(formatSessionLength(12 * 3_600)).toBe("12 hours");
    expect(formatSessionLength(36 * 3_600)).toBe("1 day 12 hours");
    expect(formatSessionLength(14 * 86_400)).toBe("14 days");
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
