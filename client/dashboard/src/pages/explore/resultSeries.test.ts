import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { describe, expect, it } from "vitest";
import {
  bucketTick,
  bucketTitle,
  isSparse,
  MAX_SERIES,
  seriesFromRows,
  sharedUnit,
  tupleLabel,
} from "./resultSeries";

const dataset: AnalyticsDataset = {
  name: "sessions",
  kind: "event",
  grain: "session",
  description: "",
  fields: [
    { name: "user", type: "string", role: "dimension", operators: ["in"] },
    {
      name: "duration_seconds",
      type: "float64",
      role: "measure",
      unit: "s",
      aggregations: ["sum"],
    },
    {
      name: "turn_count",
      type: "int64",
      role: "measure",
      aggregations: ["sum"],
    },
  ],
};

describe("seriesFromRows", () => {
  it("pivots bucketed rows into one series per tuple, sorted buckets, gaps as null", () => {
    const set = seriesFromRows(
      [
        { time_bucket: "2026-09-14T11:00:00Z", user: "bob", count: 2 },
        { time_bucket: "2026-09-14T10:00:00Z", user: "ann", count: 5 },
        { time_bucket: "2026-09-14T11:00:00Z", user: "ann", count: 1 },
      ],
      ["user"],
      [{ op: "count", field: "" }],
      dataset,
    );
    expect(set.buckets).toEqual([
      "2026-09-14T10:00:00Z",
      "2026-09-14T11:00:00Z",
    ]);
    expect(set.series).toEqual([
      { label: "ann", unit: "", points: [5, 1] },
      { label: "bob", unit: "", points: [null, 2] },
    ]);
    expect(set.hidden).toBe(0);
  });

  it("names series by measure when there are several, with the tuple appended when there is one", () => {
    const rows = [
      { time_bucket: "t1", user: "ann", count: 5, sum_turn_count: 9 },
    ];
    const measures = [
      { op: "count" as const, field: "" },
      { op: "sum" as const, field: "turn_count" },
    ];
    expect(
      seriesFromRows(rows, ["user"], measures, dataset).series.map(
        (s) => s.label,
      ),
    ).toEqual(["SUM(turn_count) · ann", "COUNT · ann"]);
    expect(
      seriesFromRows(
        [{ time_bucket: "t1", count: 5, sum_turn_count: 9 }],
        [],
        measures,
        dataset,
      ).series.map((s) => s.label),
    ).toEqual(["SUM(turn_count)", "COUNT"]);
  });

  it("keeps the largest series and counts the rest as hidden", () => {
    const rows = Array.from({ length: MAX_SERIES + 3 }, (_, i) => ({
      time_bucket: "t1",
      user: `user-${i}`,
      count: i,
    }));
    const set = seriesFromRows(
      rows,
      ["user"],
      [{ op: "count", field: "" }],
      dataset,
    );
    expect(set.series).toHaveLength(MAX_SERIES);
    expect(set.series[0]?.label).toBe(`user-${MAX_SERIES + 2}`);
    expect(set.hidden).toBe(3);
  });

  it("labels the empty tuple as all and blanks as a dash", () => {
    expect(tupleLabel({ count: 1 }, [])).toBe("all");
    expect(tupleLabel({ user: "", surface: "cli" }, ["user", "surface"])).toBe(
      "— · cli",
    );
  });
});

describe("sharedUnit", () => {
  it("is the one unit the measures share, or null", () => {
    expect(sharedUnit(dataset, [{ op: "count", field: "" }])).toBe("");
    expect(
      sharedUnit(dataset, [
        { op: "sum", field: "duration_seconds" },
        { op: "sum", field: "duration_seconds" },
      ]),
    ).toBe("s");
    expect(
      sharedUnit(dataset, [
        { op: "count", field: "" },
        { op: "sum", field: "duration_seconds" },
      ]),
    ).toBeNull();
  });
});

describe("bucket labels", () => {
  const localeDate = (date: Date) =>
    date.toLocaleDateString(undefined, { month: "short", day: "numeric" });

  it("reads daily grains as dates and hourly grains as times", () => {
    const noon = new Date(2026, 8, 14, 12, 0);
    expect(bucketTick(noon.toISOString(), "day")).toBe(localeDate(noon));
    expect(bucketTick(noon.toISOString(), "hour")).toBe("12:00");
    expect(bucketTitle(noon.toISOString(), "day")).toBe(localeDate(noon));
    expect(bucketTitle(noon.toISOString(), "hour")).toContain("12:00");
  });

  it("marks a local midnight bucket with its date even at an hourly grain", () => {
    const midnight = new Date(2026, 8, 14, 0, 0);
    expect(bucketTick(midnight.toISOString(), "hour")).toBe(
      localeDate(midnight),
    );
  });
});

describe("isSparse", () => {
  it("is true up to three points, so a lone bucket still shows", () => {
    expect(isSparse([null, 2, null])).toBe(true);
    expect(isSparse([1, 2, 3])).toBe(true);
    expect(isSparse([1, 2, 3, 4])).toBe(false);
  });
});
