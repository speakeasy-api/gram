import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { dateTimeFormatters, formatRelativeTime } from "./dates";

const NOW = new Date("2026-07-27T12:00:00Z");

describe("formatRelativeTime", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const minutesAgo = (mins: number) => new Date(NOW.getTime() - mins * 60_000);

  it("returns null for null input", () => {
    expect(formatRelativeTime(null)).toBeNull();
  });

  it("labels sub-minute recency as just now", () => {
    expect(formatRelativeTime(minutesAgo(0))).toBe("just now");
    expect(formatRelativeTime(new Date(NOW.getTime() - 59_000))).toBe(
      "just now",
    );
  });

  it("floors minutes and hours instead of rounding up", () => {
    // A recency label must never claim more elapsed time than actually
    // passed: 90 minutes reads 1h, not 2h.
    expect(formatRelativeTime(minutesAgo(90))).toBe("1h ago");
    expect(formatRelativeTime(minutesAgo(59))).toBe("59m ago");
    expect(formatRelativeTime(minutesAgo(23 * 60))).toBe("23h ago");
    expect(formatRelativeTime(minutesAgo(36 * 60))).toBe("1d ago");
  });

  it("clamps future timestamps to just now", () => {
    expect(formatRelativeTime(minutesAgo(-5))).toBe("just now");
  });
});

describe("dateTimeFormatters.humanize", () => {
  const humanize = (date: Date, includeTime: boolean) =>
    dateTimeFormatters.humanize(date, { referenceDate: NOW, includeTime });

  it("drops the clock time from a date in an earlier year", () => {
    // A domain registered in 2001 is a date, not a moment: rendering it as
    // "5 Jul 2001, 03:41" claims a precision the registry answer never had.
    const registered = new Date("2001-07-05T03:41:00Z");

    expect(humanize(registered, false)).not.toMatch(/\d:\d/);
    expect(humanize(registered, true)).toMatch(/\d:\d/);
  });

  it("keeps the year on a date-only render of an earlier year", () => {
    expect(humanize(new Date("2001-07-05T03:41:00Z"), false)).toContain("2001");
  });
});
