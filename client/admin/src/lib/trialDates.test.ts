import { afterEach, describe, expect, it, vi } from "vitest";

import {
  calendarDate,
  dayISO,
  dayOf,
  formatTrialTimeRemaining,
  trialEndDay,
  utcTodayDay,
} from "./trialDates";
import { fmtDateShort } from "./utils";

// The same device `utils.test.ts` uses, and for the same reason: CI runs in UTC,
// where every fault this file pins is invisible.
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

// The day the operator is offered by default, and one on the other side of a
// daylight-saving boundary from it.
const ANCHOR = "2026-05-06T03:00:00Z";

describe("trialEndDay", () => {
  it("reads the server's day, not the reader's", () => {
    inZone("America/Los_Angeles", () => {
      // The zone really moved: locally this instant is the 5th. Without this
      // the assertion below passes on a UTC runner for the wrong reason.
      expect(new Date(ANCHOR).getDate()).toBe(5);

      expect(trialEndDay(ANCHOR)).toBe(dayOf(new Date(2026, 4, 6)));
    });
  });

  it("reads one instant as one day in every zone", () => {
    const zones = ["UTC", "America/Los_Angeles", "Asia/Tokyo", "Pacific/Apia"];

    const read = new Set(
      zones.map((tz) => inZone(tz, () => trialEndDay(ANCHOR))),
    );

    expect(read.size).toBe(1);
  });

  it("has no answer for a trial with no end", () => {
    // The caller keeps its day-count field on this: an anchor guessed from
    // today would extend from a date the server is not holding.
    expect(trialEndDay(undefined)).toBeUndefined();
    expect(trialEndDay("")).toBeUndefined();
    expect(trialEndDay("not a date")).toBeUndefined();
  });
});

describe("the count of days between two dates", () => {
  it("counts whole days across a daylight-saving boundary", () => {
    // The fault this module exists for. 2026-03-08 is the spring forward in
    // America/Los_Angeles, so the fortnight after the 1st is 13 hours short of
    // 14 days measured as instants, and floors to 13.
    inZone("America/Los_Angeles", () => {
      const from = new Date(2026, 2, 1);
      const to = new Date(2026, 2, 15);

      expect((to.getTime() - from.getTime()) / 86_400_000).toBeLessThan(14);
      expect(dayOf(to) - dayOf(from)).toBe(14);
    });
  });

  it("counts whole days across the autumn boundary too", () => {
    // The other direction, which rounds the other way and so survives a
    // `Math.round` that the spring case kills.
    inZone("America/Los_Angeles", () => {
      const from = new Date(2026, 9, 25);
      const to = new Date(2026, 10, 8);

      expect((to.getTime() - from.getTime()) / 86_400_000).toBeGreaterThan(14);
      expect(dayOf(to) - dayOf(from)).toBe(14);
    });
  });

  it("counts a year of a trial extension", () => {
    inZone("Asia/Tokyo", () => {
      const anchor = trialEndDay(ANCHOR);
      if (anchor === undefined) throw new Error("no anchor");

      expect(dayOf(new Date(2027, 4, 6)) - anchor).toBe(365);
    });
  });
});

describe("utcTodayDay", () => {
  it("reads the UTC day, not the reader's", () => {
    inZone("America/Los_Angeles", () => {
      const now = new Date("2026-01-16T03:00:00Z");
      // The zone really moved: locally this instant is the 15th. Without this
      // the assertion below passes on a UTC runner for the wrong reason.
      expect(now.getDate()).toBe(15);

      expect(utcTodayDay(now)).toBe(dayOf(new Date(2026, 0, 16)));
    });
  });

  it("reads one instant as one day in every zone", () => {
    const now = new Date("2026-01-16T03:00:00Z");
    const zones = ["UTC", "America/Los_Angeles", "Asia/Tokyo", "Pacific/Apia"];

    const read = new Set(zones.map((tz) => inZone(tz, () => utcTodayDay(now))));

    expect(read.size).toBe(1);
  });
});

describe("calendarDate", () => {
  it("hands a day back as the local midnight a calendar selects", () => {
    inZone("America/Los_Angeles", () => {
      const anchor = trialEndDay(ANCHOR);
      if (anchor === undefined) throw new Error("no anchor");

      const picked = calendarDate(anchor + 14);

      // Local fields, because that is what a calendar reads off a date it was
      // handed and what it compares its own days against.
      expect(picked.getFullYear()).toBe(2026);
      expect(picked.getMonth()).toBe(4);
      expect(picked.getDate()).toBe(20);
      expect(picked.getHours()).toBe(0);
    });
  });

  it("survives the round trip in every zone", () => {
    for (const tz of ["UTC", "America/Los_Angeles", "Pacific/Apia"]) {
      inZone(tz, () => {
        const anchor = trialEndDay(ANCHOR);
        if (anchor === undefined) throw new Error("no anchor");

        for (const days of [1, 14, 365]) {
          expect(dayOf(calendarDate(anchor + days))).toBe(anchor + days);
        }
      });
    }
  });
});

describe("dayISO", () => {
  it("names a day the way the rest of the app renders one", () => {
    inZone("America/Los_Angeles", () => {
      const anchor = trialEndDay(ANCHOR);
      if (anchor === undefined) throw new Error("no anchor");

      // The trigger's label and the record's Plan group have to agree: this is
      // the whole reason the trigger goes through `fmtDateShort` rather than
      // through the picked date's own `toLocaleDateString`.
      expect(fmtDateShort(dayISO(anchor))).toBe(fmtDateShort(ANCHOR));
      expect(fmtDateShort(dayISO(anchor + 14))).toBe(
        fmtDateShort("2026-05-20T12:00:00Z"),
      );
    });
  });
});

const NOW = new Date("2026-01-01T00:00:00Z");

function after(milliseconds: number): string {
  return new Date(NOW.getTime() + milliseconds).toISOString();
}

describe("formatTrialTimeRemaining", () => {
  it("uses ceiling days above 72 hours", () => {
    expect(formatTrialTimeRemaining(after(72 * 60 * 60 * 1000 + 1), NOW)).toBe(
      "4 days",
    );
    expect(formatTrialTimeRemaining(after(96 * 60 * 60 * 1000), NOW)).toBe(
      "4 days",
    );
  });

  it("uses ceiling hours from 24 through 72 hours, inclusive", () => {
    expect(formatTrialTimeRemaining(after(24 * 60 * 60 * 1000), NOW)).toBe(
      "24 hours",
    );
    expect(formatTrialTimeRemaining(after(24 * 60 * 60 * 1000 + 1), NOW)).toBe(
      "25 hours",
    );
    expect(formatTrialTimeRemaining(after(72 * 60 * 60 * 1000), NOW)).toBe(
      "72 hours",
    );
  });

  it("uses ceiling total minutes below 24 hours", () => {
    expect(formatTrialTimeRemaining(after((2 * 60 + 1) * 60 * 1000), NOW)).toBe(
      "2 hours 1 minute",
    );
    expect(formatTrialTimeRemaining(after(60 * 1000), NOW)).toBe("1 minute");
    expect(formatTrialTimeRemaining(after(1), NOW)).toBe("1 minute");
  });

  it("carries rounded minutes into hours", () => {
    expect(formatTrialTimeRemaining(after(60 * 60 * 1000 - 1), NOW)).toBe(
      "1 hour",
    );
    expect(formatTrialTimeRemaining(after(24 * 60 * 60 * 1000 - 1), NOW)).toBe(
      "24 hours",
    );
  });

  it("has no answer for invalid or elapsed ranges", () => {
    expect(formatTrialTimeRemaining(undefined, NOW)).toBeUndefined();
    expect(formatTrialTimeRemaining("not a date", NOW)).toBeUndefined();
    expect(
      formatTrialTimeRemaining(after(60_000), new Date("not a date")),
    ).toBeUndefined();
    expect(formatTrialTimeRemaining(NOW.toISOString(), NOW)).toBeUndefined();
    expect(
      formatTrialTimeRemaining("2025-12-31T23:59:59Z", NOW),
    ).toBeUndefined();
  });
});
