import { afterAll, describe, expect, it, vi } from "vitest";
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

// Pin the zone and locale before the formatters are built at import.
const pinned = vi.hoisted(() => {
  const tz = process.env.TZ;
  const Original = Intl.DateTimeFormat;
  process.env.TZ = "UTC";
  const Pinned = function (
    locales?: Intl.LocalesArgument,
    options?: Intl.DateTimeFormatOptions,
  ) {
    return new Original(locales ?? "en-GB", options);
  } as unknown as typeof Intl.DateTimeFormat;
  Pinned.supportedLocalesOf = (locales, options) =>
    Original.supportedLocalesOf(locales, options);
  Intl.DateTimeFormat = Pinned;
  return { tz, Original };
});

afterAll(() => {
  Intl.DateTimeFormat = pinned.Original;
  if (pinned.tz === undefined) delete process.env.TZ;
  else process.env.TZ = pinned.tz;
});

describe("sourceSentence", () => {
  it("says where the failures came from in a sentence true for every reason with that source", () => {
    expect(sourceSentence("speakeasy_setting")).toBe(
      "A Speakeasy admin should look into these failures.",
    );
    expect(sourceSentence("service", "mcp.linear.app")).toBe(
      "These failures came from the service (mcp.linear.app).",
    );
    expect(sourceSentence("service")).toBe(
      "These failures came from the service.",
    );
    expect(sourceSentence("app", "Cursor 2.0.1")).toBe(
      "These failures point to the app (Cursor 2.0.1).",
    );
    expect(sourceSentence("server")).toBe(
      "These failures came from the MCP server.",
    );
    expect(sourceSentence("identity_provider")).toBe(
      "These failures came from the identity provider, or it could not be reached.",
    );
    expect(sourceSentence("speakeasy")).toBe(
      "Speakeasy should look into these failures.",
    );
  });
});

describe("formatAt", () => {
  const now = new Date("2026-11-20T12:00:00Z");

  it("shows only the time today", () => {
    expect(formatAt("2026-11-20T09:48:00Z", now)).toBe("09:48");
  });

  it("adds the date on another day", () => {
    expect(formatAt("2026-11-18T16:05:00Z", now)).toBe("18 Nov, 16:05");
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
