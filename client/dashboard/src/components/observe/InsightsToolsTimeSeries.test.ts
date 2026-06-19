import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { buildToolUsageTimeSeries } from "./toolUsageTimeSeriesChartData";

describe("buildToolUsageTimeSeries", () => {
  it("preserves bucketed chart data and returns timestamps for zoom", () => {
    const first = Date.UTC(2026, 0, 1, 12, 0);
    const second = Date.UTC(2026, 0, 1, 13, 0);

    const result = buildToolUsageTimeSeries(
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
      second - first,
    );

    expect(result.timestamps).toEqual([first, second]);
    expect(result.datasets[0]?.data).toEqual([3, 5]);
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
          bucketStartNs: `${BigInt(valid) * BigInt(1_000_000)}`,
          eventCount: 5,
          targetLabel: "Server A",
        },
      ],
      (point) => point.targetLabel,
      60 * 60 * 1000,
    );

    expect(result.timestamps).toEqual([valid]);
    expect(result.datasets[0]?.data).toEqual([5]);
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
