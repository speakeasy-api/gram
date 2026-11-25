// @vitest-environment node
import { describe, expect, it } from "vitest";

import { OUTCOME } from "@/components/chart/palette";
import {
  buildToolOutcomeTimeSeries,
  toolOutcomeDatasets,
} from "./toolUsageTimeSeriesChartData";

const HOUR_MS = 3_600_000;
const from = new Date("2026-11-01T00:00:00Z");
const ns = (date: Date) => String(BigInt(date.getTime()) * 1_000_000n);
const point = (
  at: Date,
  counts: {
    event: number;
    failed?: number;
    blocked?: number;
    pending?: number;
  },
) => ({
  bucketStartNs: ns(at),
  eventCount: counts.event,
  failureCount: counts.failed ?? 0,
  blockedCount: counts.blocked ?? 0,
  pendingCount: counts.pending ?? 0,
});

describe("toolOutcomeDatasets", () => {
  it("splits calls by outcome in the shared outcome colors", () => {
    const datasets = toolOutcomeDatasets(
      [
        point(from, { event: 10, failed: 3, blocked: 1, pending: 2 }),
        // Two targets in one bucket add up.
        point(from, { event: 5, failed: 1 }),
      ],
      { fromMs: from.getTime(), bucketMs: HOUR_MS, bucketCount: 2 },
      "grey",
    );
    expect(
      datasets.map(({ label, data, backgroundColor }) => [
        label,
        data,
        backgroundColor,
      ]),
    ).toEqual([
      ["Succeeded", [8, 0], OUTCOME.ok],
      ["Failed", [4, 0], OUTCOME.failed],
      ["Blocked", [1, 0], OUTCOME.blocked],
      ["Pending", [2, 0], "grey"],
    ]);
  });

  it("keeps each outcome's color when other outcomes are missing", () => {
    // Color follows the outcome, never its rank: with no successes, failures
    // must not move into the first slot's color.
    const datasets = toolOutcomeDatasets(
      [point(from, { event: 4, failed: 4 })],
      { fromMs: from.getTime(), bucketMs: HOUR_MS, bucketCount: 1 },
      "grey",
    );
    expect(datasets.map(({ label }) => label)).toEqual(["Failed"]);
    expect(datasets[0]?.backgroundColor).toBe(OUTCOME.failed);
  });
});

describe("toolOutcomeDatasets edges", () => {
  it("counts a bucket that starts before the window in the first bar", () => {
    const datasets = toolOutcomeDatasets(
      [point(new Date(from.getTime() - 30 * 60_000), { event: 2, failed: 1 })],
      { fromMs: from.getTime(), bucketMs: HOUR_MS, bucketCount: 2 },
      "grey",
    );
    expect(datasets.find((d) => d.label === "Failed")?.data).toEqual([1, 0]);
    expect(datasets.find((d) => d.label === "Succeeded")?.data).toEqual([1, 0]);
  });

  it("never counts negative successes when outcomes exceed the total", () => {
    const datasets = toolOutcomeDatasets(
      [point(from, { event: 3, failed: 2, blocked: 1, pending: 1 })],
      { fromMs: from.getTime(), bucketMs: HOUR_MS, bucketCount: 1 },
      "grey",
    );
    expect(datasets.map(({ label }) => label)).toEqual([
      "Failed",
      "Blocked",
      "Pending",
    ]);
  });
});

describe("buildToolOutcomeTimeSeries", () => {
  it("returns no grid when there are no calls", () => {
    const chart = buildToolOutcomeTimeSeries(
      [],
      from,
      new Date(from.getTime() + 24 * HOUR_MS),
      "grey",
    );
    expect(chart.datasets).toEqual([]);
    expect(chart.labels).toEqual([]);
  });

  it("lays the calls on a dense grid over the window", () => {
    const to = new Date(from.getTime() + 6 * HOUR_MS);
    const chart = buildToolOutcomeTimeSeries(
      [point(new Date(from.getTime() + 2 * HOUR_MS), { event: 3, failed: 1 })],
      from,
      to,
      "grey",
    );
    expect(chart.timestamps).toHaveLength(7);
    expect(chart.datasets.find((d) => d.label === "Failed")?.data[2]).toBe(1);
    expect(chart.datasets.find((d) => d.label === "Succeeded")?.data[2]).toBe(
      2,
    );
  });
});
