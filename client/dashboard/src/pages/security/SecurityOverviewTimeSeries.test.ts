import { describe, expect, it } from "vitest";
import { buildRiskTrendSeries } from "./riskTrendChartData";

describe("buildRiskTrendSeries", () => {
  it("returns one timestamped series per category, ordered and labeled", () => {
    const first = new Date(Date.UTC(2026, 0, 1, 12, 0));
    const second = new Date(Date.UTC(2026, 0, 1, 13, 0));

    const series = buildRiskTrendSeries([
      { category: "secrets", bucketStart: first, findings: 2 },
      { category: "secrets", bucketStart: second, findings: 4 },
    ]);

    expect(series).toHaveLength(1);
    expect(series[0]?.label).toBe("Secrets");
    expect(series[0]?.data).toEqual([
      { x: first.getTime(), y: 2 },
      { x: second.getTime(), y: 4 },
    ]);
  });

  it("orders known categories before unknown ones, alphabetically within each", () => {
    const t = new Date(Date.UTC(2026, 0, 1, 12, 0));

    const series = buildRiskTrendSeries([
      { category: "custom", bucketStart: t, findings: 1 },
      { category: "financial", bucketStart: t, findings: 1 },
      { category: "secrets", bucketStart: t, findings: 1 },
      { category: "zzz_unknown", bucketStart: t, findings: 1 },
    ]);

    expect(series.map((s) => s.label)).toEqual([
      "Secrets",
      "Financial Information",
      "Custom Rules",
      "zzz_unknown",
    ]);
  });
});
