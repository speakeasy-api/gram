import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { buildToolUsageTimeSeries } from "./toolUsageTimeSeriesChartData";

describe("buildToolUsageTimeSeries", () => {
  it("buckets events into one series per key", () => {
    const first = Date.UTC(2026, 0, 1, 12, 0);
    const second = Date.UTC(2026, 0, 1, 13, 0);

    const series = buildToolUsageTimeSeries(
      [
        {
          bucketStartNs: `${BigInt(first) * BigInt(1_000_000)}`,
          eventCount: 3,
          targetLabel: "Server A",
        },
        {
          bucketStartNs: `${BigInt(second) * BigInt(1_000_000)}`,
          eventCount: 5,
          targetLabel: "Server A",
        },
      ],
      (point) => point.targetLabel,
    );

    expect(series).toHaveLength(1);
    expect(series[0]?.label).toBe("Server A");
    expect(series[0]?.data).toEqual([
      { x: first, y: 3 },
      { x: second, y: 5 },
    ]);
  });

  it("skips malformed bucket timestamps instead of throwing", () => {
    const valid = Date.UTC(2026, 0, 1, 12, 0);

    const series = buildToolUsageTimeSeries(
      [
        {
          bucketStartNs: "not-a-nanosecond-timestamp",
          eventCount: 3,
          targetLabel: "Server A",
        },
        {
          bucketStartNs: `${BigInt(valid) * BigInt(1_000_000)}`,
          eventCount: 5,
          targetLabel: "Server A",
        },
      ],
      (point) => point.targetLabel,
    );

    expect(series[0]?.data).toEqual([{ x: valid, y: 5 }]);
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
});
