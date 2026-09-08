import { afterEach, describe, expect, it, vi } from "vitest";

import { byOldestFirst, fmtDateShort } from "./utils";

// Node re-reads the TZ variable when it next builds a Date, so the reader's
// zone can be moved for the length of a call. Written out rather than left to
// the machine: CI runs in UTC, where the fault this pins cannot appear at all
// and every assertion about it would pass without meaning anything.
//
// Through `vi.stubEnv` rather than `process.env` directly, because the browser
// build carries no node types and its restore is what the hook below relies on.
function inZone<T>(tz: string, body: () => T): T {
  vi.stubEnv("TZ", tz);
  try {
    return body();
  } finally {
    vi.unstubAllEnvs();
  }
}

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("fmtDateShort", () => {
  it("reads a trial end as the server's day, not the reader's", () => {
    // A trial end as the server stores it: the signup moment plus the trial
    // length, in UTC. Early in the UTC day, which is where the fault shows.
    const iso = "2026-05-06T03:00:00Z";

    inZone("America/Los_Angeles", () => {
      // The zone really moved, and in it this instant is the 5th locally.
      // Without this the rest of the test proves nothing on a UTC runner.
      expect(new Date(iso).getDate()).toBe(5);

      // The regression itself: the reader's own zone is what shipped, and it
      // names the day before the deadline.
      expect(fmtDateShort(iso)).not.toBe(new Date(iso).toLocaleDateString());

      // Midday UTC on the same date is the 6th in every populated zone, so it
      // fixes the expected day without hard-coding a locale's date format.
      expect(fmtDateShort(iso)).toBe(
        new Date("2026-05-06T12:00:00Z").toLocaleDateString(),
      );
    });
  });

  it("reads the same instant the same way in every zone", () => {
    const iso = "2026-05-06T03:00:00Z";
    const zones = ["UTC", "America/Los_Angeles", "Asia/Tokyo", "Pacific/Apia"];

    const rendered = new Set(
      zones.map((tz) => inZone(tz, () => fmtDateShort(iso))),
    );

    // One reading, so two operators on two continents act on one date.
    expect(rendered.size).toBe(1);
  });

  it("reads an unset date as a dash", () => {
    expect(fmtDateShort(undefined)).toBe("-");
    expect(fmtDateShort("")).toBe("-");
  });

  it("reads an unparseable date as a dash", () => {
    // Not the literal string `Invalid Date`, which is what dropping the guard
    // puts in front of an operator.
    expect(fmtDateShort("not a date")).toBe("-");
    expect(fmtDateShort("2026-13-45T00:00:00Z")).toBe("-");
  });

  it("drops the clock", () => {
    const rendered = fmtDateShort("2026-05-06T13:45:12Z");

    expect(rendered).toBe(fmtDateShort("2026-05-06T00:00:00Z"));
    expect(rendered).not.toContain(":");
  });
});

describe("byOldestFirst", () => {
  const row = (id: string, created_at: string) => ({ id, created_at });

  it("puts the older record first", () => {
    expect(
      byOldestFirst(
        row("b", "2026-02-01T00:00:00Z"),
        row("a", "2026-01-01T00:00:00Z"),
      ),
    ).toBeGreaterThan(0);
    expect(
      byOldestFirst(
        row("a", "2026-01-01T00:00:00Z"),
        row("b", "2026-02-01T00:00:00Z"),
      ),
    ).toBeLessThan(0);
  });

  it("reads the instant, not the string", () => {
    // The same moment, written two ways. A comparator that compares the text
    // orders these by their offset instead of by when they happened.
    expect(
      byOldestFirst(
        row("a", "2026-01-01T00:00:00Z"),
        row("b", "2025-12-31T16:00:00-08:00"),
      ),
    ).toBe("a".localeCompare("b"));
  });

  it("breaks a tie on the id, so equal rows keep one order", () => {
    const same = "2026-01-01T00:00:00Z";
    expect(byOldestFirst(row("a", same), row("b", same))).toBeLessThan(0);
    expect(byOldestFirst(row("b", same), row("a", same))).toBeGreaterThan(0);
    expect(byOldestFirst(row("a", same), row("a", same))).toBe(0);
  });

  it("falls back to the id rather than shuffling around an unparseable date", () => {
    // NaN compares false against everything, so an arithmetic comparator
    // returns NaN here and the sort order becomes whatever the engine did last.
    expect(
      byOldestFirst(row("a", "not a date"), row("b", "2026-01-01T00:00:00Z")),
    ).toBeLessThan(0);
    expect(
      byOldestFirst(row("b", "not a date"), row("a", "not a date")),
    ).toBeGreaterThan(0);
  });
});
