import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  buildToolUsageTimeSeries,
  pickTimeBucketMs,
} from "./toolUsageTimeSeriesChartData";

const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;

function nsFor(ms: number): string {
  return `${BigInt(ms) * BigInt(1_000_000)}`;
}

describe("pickTimeBucketMs", () => {
  it("keeps hourly buckets for short ranges", () => {
    expect(pickTimeBucketMs(24 * HOUR_MS)).toBe(HOUR_MS);
  });

  it("coarsens buckets so long ranges stay under the bucket cap", () => {
    expect(pickTimeBucketMs(7 * DAY_MS)).toBe(6 * HOUR_MS);
    expect(pickTimeBucketMs(90 * DAY_MS)).toBe(2 * DAY_MS);
  });
});

describe("buildToolUsageTimeSeries", () => {
  it("preserves bucketed chart data and returns timestamps for zoom", () => {
    const first = Date.UTC(2026, 0, 1, 12, 0);
    const second = Date.UTC(2026, 0, 1, 13, 0);

    const result = buildToolUsageTimeSeries(
      [
        {
          bucketStartNs: nsFor(first),
          eventCount: 3,
          targetLabel: "Server A",
        },
        {
          bucketStartNs: nsFor(second),
          eventCount: 5,
          targetLabel: "Server A",
        },
      ],
      (point) => point.targetLabel,
      new Date(first),
      new Date(second),
    );

    expect(result.timestamps).toEqual([first, second]);
    expect(result.datasets[0]?.data).toEqual([3, 5]);
  });

  it("fills quiet buckets with zeros so the time axis stays dense", () => {
    const from = Date.UTC(2026, 0, 1, 12, 0);

    const result = buildToolUsageTimeSeries(
      [
        {
          bucketStartNs: nsFor(from),
          eventCount: 2,
          targetLabel: "Server A",
        },
        {
          bucketStartNs: nsFor(from + 3 * HOUR_MS),
          eventCount: 4,
          targetLabel: "Server A",
        },
      ],
      (point) => point.targetLabel,
      new Date(from),
      new Date(from + 3 * HOUR_MS),
    );

    expect(result.timestamps).toEqual([
      from,
      from + HOUR_MS,
      from + 2 * HOUR_MS,
      from + 3 * HOUR_MS,
    ]);
    expect(result.datasets[0]?.data).toEqual([2, 0, 0, 4]);
  });

  it("re-aggregates fine-grained points into coarser buckets for long ranges", () => {
    const from = Date.UTC(2026, 0, 1);
    const to = from + 7 * DAY_MS;

    // Hourly points across a 7-day range should collapse into 6h buckets.
    const points = [0, 1, 2, 3, 4, 5].map((hour) => ({
      bucketStartNs: nsFor(from + hour * HOUR_MS),
      eventCount: 1,
      targetLabel: "Server A",
    }));

    const result = buildToolUsageTimeSeries(
      points,
      (point) => point.targetLabel,
      new Date(from),
      new Date(to),
    );

    expect(result.bucketMs).toBe(6 * HOUR_MS);
    expect(result.datasets[0]?.data[0]).toBe(6);
    expect(result.timestamps.length).toBeLessThanOrEqual(49);
  });

  it("skips malformed bucket timestamps instead of throwing", () => {
    const valid = Date.UTC(2026, 0, 1, 12, 0);

    const result = buildToolUsageTimeSeries(
      [
        {
          bucketStartNs: "not-a-nanosecond-timestamp",
          eventCount: 3,
          targetLabel: "Server A",
        },
        {
          bucketStartNs: nsFor(valid),
          eventCount: 5,
          targetLabel: "Server A",
        },
      ],
      (point) => point.targetLabel,
      new Date(valid),
      new Date(valid + HOUR_MS),
    );

    expect(result.timestamps).toEqual([valid, valid + HOUR_MS]);
    expect(result.datasets[0]?.data).toEqual([5, 0]);
  });

  it("folds series beyond the palette into an Other bucket instead of cycling colors", () => {
    const from = Date.UTC(2026, 0, 1, 12, 0);
    const colors = ["#111111", "#222222", "#333333"];

    const points = ["a", "b", "c", "d", "e"].map((label, i) => ({
      bucketStartNs: nsFor(from),
      eventCount: 10 - i,
      targetLabel: label,
    }));

    const result = buildToolUsageTimeSeries(
      points,
      (point) => point.targetLabel,
      new Date(from),
      new Date(from + HOUR_MS),
      undefined,
      colors,
    );

    expect(result.datasets.map((ds) => ds.label)).toEqual(["a", "b", "Other"]);
    // c + d + e event counts merge into Other.
    expect(result.datasets[2]?.data[0]).toBe(8 + 7 + 6);
    const usedColors = result.datasets.map((ds) => ds.backgroundColor);
    expect(new Set(usedColors).size).toBe(usedColors.length);
  });
});

describe("InsightsTools chart zoom wiring", () => {
  it("passes range-selection controls to the Skill Usage chart", () => {
    const source = readFileSync(
      "src/components/observe/InsightsTools.tsx",
      "utf8",
    );

    const callsite = source.match(/<SkillUsageTimeSeries\b[\s\S]*?\/>/)?.[0];

    expect(callsite).toContain("onRangeSelect={onRangeSelect}");
    expect(callsite).toContain("isZoomed={isZoomed}");
    expect(callsite).toContain("onResetZoom={onResetZoom}");
  });

  it("filters zero-value series out of the stacked bar tooltip", () => {
    const source = readFileSync(
      "src/components/observe/InsightsTools.tsx",
      "utf8",
    );

    // Guard against regressing to `return undefined` in the label callback —
    // Chart.js treats that as "use default" and re-lists every series,
    // ballooning the tooltip over the chart (DNO-717).
    expect(source).toMatch(
      /filter:\s*\(item\)\s*=>\s*\(item\.parsed\.y\s*\?\?\s*0\)\s*!==\s*0/,
    );
    expect(source).not.toMatch(
      /if \(\(item\.parsed\.y \?\? 0\) === 0\) return undefined;/,
    );
  });
});
