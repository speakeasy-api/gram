import { describe, expect, it } from "vitest";

import { withinRecentWindow, withinUpcomingWindow } from "./session-filters";

const NOW = new Date("2026-08-08T12:00:00Z").getTime();
const hoursFromNow = (hours: number) => new Date(NOW + hours * 60 * 60 * 1000);

describe("withinRecentWindow", () => {
  it("matches everything when no window is selected", () => {
    expect(withinRecentWindow(hoursFromNow(-24 * 365), null, NOW)).toBe(true);
  });

  it("keeps a date inside the window", () => {
    expect(withinRecentWindow(hoursFromNow(-23), "24h", NOW)).toBe(true);
  });

  it("drops a date older than the window", () => {
    expect(withinRecentWindow(hoursFromNow(-25), "24h", NOW)).toBe(false);
  });

  // Rows are created in the past, but a clock skew between the server and the
  // browser can put one marginally ahead; it must not fall out of every window.
  it("keeps a date in the immediate future", () => {
    expect(withinRecentWindow(hoursFromNow(1), "24h", NOW)).toBe(true);
  });

  it("matches everything when the window is unrecognized", () => {
    expect(withinRecentWindow(hoursFromNow(-24 * 365), "bogus", NOW)).toBe(
      true,
    );
  });
});

describe("withinUpcomingWindow", () => {
  it("matches everything when no window is selected", () => {
    expect(withinUpcomingWindow(hoursFromNow(24 * 365), null, NOW)).toBe(true);
  });

  it("keeps a deadline inside the window", () => {
    expect(withinUpcomingWindow(hoursFromNow(23), "24h", NOW)).toBe(true);
  });

  it("drops a deadline beyond the window", () => {
    expect(withinUpcomingWindow(hoursFromNow(25), "24h", NOW)).toBe(false);
  });

  // "Expires within 24 hours" has to include a session that already lapsed:
  // it is not expiring later than 24 hours from now, and hiding it would make
  // the most urgent rows the hardest to find.
  it("keeps a deadline that has already passed", () => {
    expect(withinUpcomingWindow(hoursFromNow(-5), "24h", NOW)).toBe(true);
  });
});
