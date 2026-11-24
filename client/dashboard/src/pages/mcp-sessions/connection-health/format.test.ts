import { describe, expect, it } from "vitest";
import {
  appKeyLabel,
  axisLabels,
  formatAt,
  formatSessionLength,
  formatWeekdayDay,
  nowPosition,
  problemRule,
  sourceSentence,
  stepResultLabel,
} from "./format";

describe("sourceSentence", () => {
  it("says who returned the failures in a plain sentence", () => {
    expect(sourceSentence("speakeasy_setting")).toBe(
      "A setting in Speakeasy caused these failures.",
    );
    expect(sourceSentence("service", "mcp.linear.app")).toBe(
      "The service (mcp.linear.app) returned these failures.",
    );
    expect(sourceSentence("service")).toBe(
      "The service returned these failures.",
    );
    expect(sourceSentence("app", "Cursor 2.0.1")).toBe(
      "The app (Cursor 2.0.1) caused these failures.",
    );
    expect(sourceSentence("server")).toBe(
      "The MCP server returned these failures.",
    );
    expect(sourceSentence("identity_provider")).toBe(
      "The identity provider returned these failures.",
    );
    expect(sourceSentence("speakeasy")).toBe(
      "These failures came from Speakeasy itself.",
    );
  });
});

describe("formatAt", () => {
  const now = new Date(2026, 10, 20, 12, 0);

  it("shows only the time today", () => {
    expect(formatAt(new Date(2026, 10, 20, 9, 48).toISOString(), now)).toMatch(
      /^\d{2}:\d{2}$/,
    );
  });

  it("adds the date on another day", () => {
    const earlier = formatAt(new Date(2026, 10, 18, 16, 5).toISOString(), now);
    expect(earlier).toMatch(/18/);
    expect(earlier).toMatch(/, \d{2}:\d{2}$/);
  });
});

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
    const date = new Date(2026, 10, 11, 12);
    const day = new Intl.DateTimeFormat(undefined, { day: "numeric" }).format(
      date,
    );
    expect(formatWeekdayDay(date.getTime()).endsWith(` ${day}`)).toBe(true);
  });
});

describe("problemRule", () => {
  it("says signed in for problems signing in to Speakeasy", () => {
    expect(
      problemRule({ rule: "one_person", part: "speakeasy" } as never),
    ).toBe(
      "One person got the same kind of failure for 24 hours and has not signed in since.",
    );
    expect(
      problemRule({ rule: "one_person", part: "service" } as never),
    ).toContain("has not connected since");
  });
});

describe("axisLabels", () => {
  const starts = (from: Date, count: number) =>
    Array.from({ length: count }, (_, i) =>
      new Date(from.getTime() + i * 10 * 60_000).toISOString(),
    );

  it("shows about five labels over a day of 10-minute buckets", () => {
    const labels = axisLabels(starts(new Date(2026, 10, 20, 0, 0), 144));
    expect(labels.filter(Boolean)).toHaveLength(5);
    expect(labels.filter(Boolean).every((l) => /^\d{2}:\d{2}$/.test(l))).toBe(
      true,
    );
  });

  it("adds the day only where it changes across midnight", () => {
    const labels = axisLabels(
      starts(new Date(2026, 10, 20, 12, 0), 144),
    ).filter(Boolean);
    const withDay = labels.filter((l) => l.includes(" "));
    expect(withDay).toHaveLength(2);
    expect(labels[0]).toContain(" ");
  });
});
