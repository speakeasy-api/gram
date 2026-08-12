import { describe, expect, it } from "vitest";
import {
  getTrialLifecycle,
  getTrialLifecycleFromDates,
  getTrialStatus,
  getTrialStatusFromDates,
} from "./trial-status";

const DAY_MS = 24 * 60 * 60 * 1000;

const trial = (startedAt: string, endsAt: string) => ({
  startedAt,
  endsAt,
});

const dateTrial = (startedAt: string, endsAt: string) => ({
  startedAt: new Date(startedAt),
  endsAt: new Date(endsAt),
});

describe("getTrialStatus", () => {
  it("returns the first day at the first instant of a 14-day trial", () => {
    expect(
      getTrialStatus(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-08-01T00:00:00.000Z"),
      ),
    ).toEqual({
      dayNumber: 1,
      totalDays: 14,
      remainingDays: 14,
      progress: 0,
    });
  });

  it("calculates progress and remaining days in the middle of a trial", () => {
    expect(
      getTrialStatus(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-08-07T12:00:00.000Z"),
      ),
    ).toEqual({
      dayNumber: 7,
      totalDays: 14,
      remainingDays: 8,
      progress: 13 / 28,
    });
  });

  it("starts each new day at its one-based day number", () => {
    const trialStart = new Date("2026-08-01T00:00:00.000Z");
    const trialEnd = new Date(trialStart.getTime() + 14 * DAY_MS);

    expect(
      getTrialStatus(
        trial(trialStart.toISOString(), trialEnd.toISOString()),
        new Date(trialStart.getTime() + DAY_MS),
      ),
    ).toMatchObject({ dayNumber: 2, remainingDays: 13 });
    expect(
      getTrialStatus(
        trial(trialStart.toISOString(), trialEnd.toISOString()),
        new Date(trialStart.getTime() + 7 * DAY_MS),
      ),
    ).toMatchObject({ dayNumber: 8, remainingDays: 7 });
  });

  it("keeps one day remaining immediately before expiry", () => {
    expect(
      getTrialStatus(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-08-14T23:59:59.999Z"),
      ),
    ).toEqual({
      dayNumber: 14,
      totalDays: 14,
      remainingDays: 1,
      progress: (14 * DAY_MS - 1) / (14 * DAY_MS),
    });
  });

  it("returns null at exact expiry", () => {
    expect(
      getTrialStatus(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-08-15T00:00:00.000Z"),
      ),
    ).toBeNull();
  });

  it("calculates an extended trial from its actual duration", () => {
    expect(
      getTrialStatus(
        trial("2026-08-01T00:00:00.000Z", "2026-08-31T00:00:00.000Z"),
        new Date("2026-08-16T00:00:00.000Z"),
      ),
    ).toEqual({
      dayNumber: 16,
      totalDays: 30,
      remainingDays: 15,
      progress: 0.5,
    });
  });

  it("clamps a pre-start reference time to the trial start", () => {
    expect(
      getTrialStatus(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-07-31T00:00:00.000Z"),
      ),
    ).toEqual({
      dayNumber: 1,
      totalDays: 14,
      remainingDays: 14,
      progress: 0,
    });
  });

  it.each([
    [undefined, new Date("2026-08-01T00:00:00.000Z")],
    [null, new Date("2026-08-01T00:00:00.000Z")],
    [trial("not-a-date", "2026-08-15T00:00:00.000Z"), new Date()],
    [trial("2026-08-01T00:00:00.000Z", "not-a-date"), new Date()],
    [
      trial("2026-08-15T00:00:00.000Z", "2026-08-01T00:00:00.000Z"),
      new Date("2026-08-01T00:00:00.000Z"),
    ],
  ])("returns null for invalid trial input: %j", (invalidTrial, now) => {
    expect(getTrialStatus(invalidTrial, now)).toBeNull();
  });
});

describe("getTrialLifecycle", () => {
  it("reports an in-progress trial as active", () => {
    expect(
      getTrialLifecycle(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-08-07T12:00:00.000Z"),
      ),
    ).toBe("active");
  });

  it("reports a trial that has not started yet as active", () => {
    expect(
      getTrialLifecycle(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-07-31T00:00:00.000Z"),
      ),
    ).toBe("active");
  });

  it("stays active in the final instant before expiry", () => {
    expect(
      getTrialLifecycle(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-08-14T23:59:59.999Z"),
      ),
    ).toBe("active");
  });

  it("reports expired at exact expiry", () => {
    expect(
      getTrialLifecycle(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-08-15T00:00:00.000Z"),
      ),
    ).toBe("expired");
  });

  it("reports a long-finished trial as expired", () => {
    expect(
      getTrialLifecycle(
        trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-09-30T00:00:00.000Z"),
      ),
    ).toBe("expired");
  });

  it.each([
    [undefined, new Date("2026-08-01T00:00:00.000Z")],
    [null, new Date("2026-08-01T00:00:00.000Z")],
    [trial("not-a-date", "2026-08-15T00:00:00.000Z"), new Date()],
    [trial("2026-08-01T00:00:00.000Z", "not-a-date"), new Date()],
    [
      trial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
      new Date(Number.NaN),
    ],
    [
      trial("2026-08-15T00:00:00.000Z", "2026-08-01T00:00:00.000Z"),
      new Date("2026-08-01T00:00:00.000Z"),
    ],
    [
      trial("2026-08-01T00:00:00.000Z", "2026-08-01T00:00:00.000Z"),
      new Date("2026-08-01T00:00:00.000Z"),
    ],
  ])("returns none for unusable trial input: %j", (invalidTrial, now) => {
    expect(getTrialLifecycle(invalidTrial, now)).toBe("none");
  });
});

describe("getTrialStatusFromDates", () => {
  it("derives the status from Date-shaped trial fields", () => {
    expect(
      getTrialStatusFromDates(
        dateTrial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date("2026-08-07T12:00:00.000Z"),
      ),
    ).toEqual({
      dayNumber: 7,
      totalDays: 14,
      remainingDays: 8,
      progress: 13 / 28,
    });
  });

  it.each([
    [undefined],
    [null],
    [{ startedAt: new Date(Number.NaN), endsAt: new Date() }],
    [{ startedAt: new Date(), endsAt: new Date(Number.NaN) }],
  ])("returns null for unusable trial input: %j", (invalidTrial) => {
    expect(
      getTrialStatusFromDates(invalidTrial, new Date("2026-08-07T12:00:00Z")),
    ).toBeNull();
  });
});

describe("getTrialLifecycleFromDates", () => {
  it.each([
    ["2026-08-07T12:00:00.000Z", "active"],
    ["2026-08-15T00:00:00.000Z", "expired"],
  ])("classifies a Date-shaped trial at %s as %s", (now, expected) => {
    expect(
      getTrialLifecycleFromDates(
        dateTrial("2026-08-01T00:00:00.000Z", "2026-08-15T00:00:00.000Z"),
        new Date(now),
      ),
    ).toBe(expected);
  });

  it.each([
    [undefined],
    [null],
    [{ startedAt: new Date(Number.NaN), endsAt: new Date() }],
    [{ startedAt: new Date(), endsAt: new Date(Number.NaN) }],
  ])("returns none for unusable trial input: %j", (invalidTrial) => {
    expect(
      getTrialLifecycleFromDates(
        invalidTrial,
        new Date("2026-08-07T12:00:00Z"),
      ),
    ).toBe("none");
  });
});
