import { describe, expect, it } from "vitest";
import type { AnalyticsQueryResult } from "@gram/client/models/components/analyticsqueryresult.js";
import { groupLabel, signalSeries } from "./explorationModel";

describe("signal chart measurements", () => {
  it("keeps missing, empty and literal missing labels distinct", () => {
    expect(groupLabel([null])).toBe("Not recorded");
    expect(groupLabel([""])).toBe("Empty value");
    expect(groupLabel(["<unset>"])).toBe("<unset>");
  });

  it("keeps unknown chart buckets null and answered negatives zero", () => {
    const result: AnalyticsQueryResult = {
      dataset: "signals",
      plan: "signals.canonical_final",
      rows: [
        {
          time_bucket: "2026-10-01T00:00:00Z",
          series_id: "a",
          matched_messages: null,
        },
        {
          time_bucket: "2026-10-01T01:00:00Z",
          series_id: "a",
          matched_messages: 0,
        },
        {
          time_bucket: "2026-10-01T02:00:00Z",
          series_id: "a",
          matched_messages: 2,
        },
      ],
      signalResult: {
        groups: [{ series_id: "a", group_values: [null] }],
        summary: {},
        metadata: {
          coverage: "unknown",
          exact: true,
          filters: [],
          from: new Date(),
          to: new Date(),
          groupBy: [{ attribute: "team" }],
          groupsTruncated: false,
          physicalResolution: "canonical",
          pointBudget: 2000,
          population: "answered",
          queriedAt: new Date(),
          selection: { sensorId: "sensor", signalId: "signal", threshold: 0.5 },
          seriesComplete: true,
          snapshot: "live",
          step: "hour",
          timeAxis: "event_occurred_at",
          trigger: "stream",
          unit: "original_message",
        },
      },
    };
    expect(signalSeries(result, "matched_messages").series[0]?.points).toEqual([
      null,
      0,
      2,
    ]);
  });
});
