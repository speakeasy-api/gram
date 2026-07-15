import { describe, expect, it } from "vitest";
import { buildAgentTokenTimeSeries } from "./agentTokenTimeSeriesChartData";

describe("buildAgentTokenTimeSeries", () => {
  it("shapes token buckets into one series per token kind", () => {
    const first = Date.UTC(2026, 0, 1, 12, 0);
    const second = Date.UTC(2026, 0, 1, 13, 0);

    const series = buildAgentTokenTimeSeries(
      [
        {
          bucketTimeUnixNano: `${BigInt(first) * BigInt(1_000_000)}`,
          totalInputTokens: 10,
          totalOutputTokens: 20,
          cacheReadInputTokens: 5,
          totalCost: 0.1,
        },
        {
          bucketTimeUnixNano: `${BigInt(second) * BigInt(1_000_000)}`,
          totalInputTokens: 30,
          totalOutputTokens: 40,
          cacheReadInputTokens: 15,
          totalCost: 0.2,
        },
      ],
      "tokens",
    );

    expect(series.map((s) => s.label)).toEqual([
      "Input Tokens",
      "Output Tokens",
      "Cache Read",
    ]);
    expect(series[0]?.data).toEqual([
      { x: first, y: 10 },
      { x: second, y: 30 },
    ]);
    expect(series[1]?.data).toEqual([
      { x: first, y: 20 },
      { x: second, y: 40 },
    ]);
    expect(series[2]?.data).toEqual([
      { x: first, y: 5 },
      { x: second, y: 15 },
    ]);
  });

  it("builds a single cost series", () => {
    const first = Date.UTC(2026, 0, 1, 12, 0);

    const series = buildAgentTokenTimeSeries(
      [
        {
          bucketTimeUnixNano: `${BigInt(first) * BigInt(1_000_000)}`,
          totalInputTokens: 10,
          totalOutputTokens: 20,
          cacheReadInputTokens: 5,
          totalCost: 0.123,
        },
      ],
      "cost",
    );

    expect(series).toHaveLength(1);
    expect(series[0]?.label).toBe("Cost");
    expect(series[0]?.data).toEqual([{ x: first, y: 0.123 }]);
  });
});
