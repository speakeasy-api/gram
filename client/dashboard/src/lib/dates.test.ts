import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { formatRelativeTime } from "./dates";

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
