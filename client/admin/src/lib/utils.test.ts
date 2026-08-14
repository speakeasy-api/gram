import { afterEach, describe, expect, it, vi } from "vitest";

import { fmtDateShort } from "./utils";

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
  it("reads a UTC midnight as that day, not the day before", () => {
    // A trial end as the API sends it: midnight UTC.
    const iso = "2026-05-06T00:00:00Z";

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
    const iso = "2026-05-06T00:00:00Z";
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
