import { describe, expect, it } from "vitest";
import { getGateCopy } from "./upgrade-gate-copy";

const NOW = new Date("2026-08-10T12:00:00.000Z");

const trial = (startedAt: string, endsAt: string) => ({
  startedAt: new Date(startedAt),
  endsAt: new Date(endsAt),
});

describe("getGateCopy", () => {
  it("names the end date once the trial has ended", () => {
    const copy = getGateCopy(
      trial("2026-07-26T12:00:00.000Z", "2026-08-09T12:00:00.000Z"),
      NOW,
    );

    expect(copy.status).toBe("Trial ended Aug 9th, 2026");
    expect(copy.dotClassName).toContain("vermilion");
    expect(copy.detail).toContain("still here when you upgrade");
  });

  it("treats the end instant itself as ended", () => {
    // The gate redirects on `now >= endsAt`, so the copy has to agree at the
    // exact boundary or the page contradicts the redirect that sent them.
    const copy = getGateCopy(
      trial("2026-07-27T12:00:00.000Z", NOW.toISOString()),
      NOW,
    );

    expect(copy.status).toBe("Trial ended Aug 10th, 2026");
  });

  it("counts down while the trial is running", () => {
    const copy = getGateCopy(
      trial("2026-08-04T12:00:00.000Z", "2026-08-18T12:00:00.000Z"),
      NOW,
    );

    expect(copy.status).toBe("Trial · 8 days left");
    expect(copy.dotClassName).toContain("moss");
    expect(copy.body).toContain("before your trial ends");
  });

  it("says day, not days, on the final day", () => {
    const copy = getGateCopy(
      trial("2026-07-28T12:00:00.000Z", "2026-08-11T12:00:00.000Z"),
      NOW,
    );

    expect(copy.status).toBe("Trial · 1 day left");
  });

  it("falls back to the running-trial wording with no trial", () => {
    const copy = getGateCopy(null, NOW);

    expect(copy.status).toBe("Trial in progress");
    expect(copy.dotClassName).toContain("moss");
  });

  it("falls back rather than rendering an unparseable date", () => {
    const copy = getGateCopy(
      { startedAt: new Date("nope"), endsAt: new Date("nope") },
      NOW,
    );

    expect(copy.status).toBe("Trial in progress");
  });
});
