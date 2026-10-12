import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { describe, expect, it } from "vitest";
import {
  filterForField,
  autoGrain,
  completeFilters,
  completeMeasures,
  drawnChart,
  fieldsForOp,
  filterableFields,
  formatMeasureValue,
  hasChartShape,
  initialSpec,
  isChartType,
  isRowsMode,
  isStacked,
  longestWindow,
  measureAlias,
  measureLabel,
  measureUnit,
  numericCell,
  operatorsForField,
  opsForDataset,
  parseLimit,
  queryBodyFromSpec,
  specForDataset,
  textCell,
  windowRange,
  type ExploreSpec,
} from "./exploreModel";

const sessions: AnalyticsDataset = {
  name: "sessions",
  kind: "event",
  grain: "session",
  description: "One row per agent session.",
  fields: [
    {
      name: "user",
      type: "string",
      role: "dimension",
      default: true,
      operators: ["equals", "in"],
      aggregations: ["count_distinct"],
    },
    {
      name: "surface",
      type: "string",
      role: "dimension",
      default: false,
      operators: ["equals", "in"],
    },
    {
      name: "turn_count",
      type: "int64",
      role: "measure",
      default: false,
      aggregations: ["sum", "avg"],
    },
    {
      name: "duration_seconds",
      type: "float64",
      role: "measure",
      default: false,
      unit: "s",
      aggregations: ["sum", "avg", "p95"],
    },
  ],
};

const usage: AnalyticsDataset = {
  name: "usage",
  kind: "metric",
  grain: "reading",
  description: "One row per metric reading.",
  fields: [
    {
      name: "model",
      type: "string",
      role: "dimension",
      default: false,
      operators: ["in"],
    },
    {
      name: "tokens",
      type: "int64",
      role: "measure",
      default: false,
      aggregations: ["sum", "max"],
    },
  ],
};

function spec(overrides: Partial<ExploreSpec> = {}): ExploreSpec {
  return { ...specForDataset(sessions), ...overrides };
}

describe("the describe-to-controls mapping", () => {
  it("offers count plus every aggregation a field declares, in a fixed order", () => {
    expect(opsForDataset(sessions)).toEqual([
      "count",
      "count_distinct",
      "sum",
      "avg",
      "p95",
    ]);
    expect(opsForDataset(usage)).toEqual(["count", "sum", "max"]);
    expect(opsForDataset(undefined)).toEqual(["count"]);
  });

  it("targets an aggregation at the fields that admit it", () => {
    expect(fieldsForOp(sessions, "count")).toEqual([]);
    expect(fieldsForOp(sessions, "count_distinct").map((f) => f.name)).toEqual([
      "user",
    ]);
    expect(fieldsForOp(sessions, "p95").map((f) => f.name)).toEqual([
      "duration_seconds",
    ]);
    expect(fieldsForOp(sessions, "sum").map((f) => f.name)).toEqual([
      "turn_count",
      "duration_seconds",
    ]);
  });

  it("filters on the fields and operators the catalog declares", () => {
    expect(filterableFields(sessions).map((f) => f.name)).toEqual([
      "user",
      "surface",
    ]);
    expect(operatorsForField(usage.fields[0])).toEqual(["in"]);
    expect(operatorsForField(undefined)).toEqual([]);
  });

  it("opens a dataset counting rows over a short window, broken down by its summary field, on the chart its kind picks", () => {
    expect(specForDataset(sessions)).toMatchObject({
      dataset: "sessions",
      measures: [{ op: "count", field: "" }],
      dimensions: ["user"],
      chartType: "line",
      window: "1d",
      limit: 0,
    });
    expect(specForDataset(usage)).toMatchObject({
      dimensions: [],
      chartType: "bar",
    });
  });

  it("keeps the window and limit when the dataset changes", () => {
    const next = specForDataset(usage, { window: "30d", limit: 25 });
    expect(next.window).toBe("30d");
    expect(next.limit).toBe(25);
  });

  it("opens on the catalog's first dataset, or nothing", () => {
    expect(initialSpec([usage, sessions])?.dataset).toBe("usage");
    expect(initialSpec([])).toBeNull();
  });

  it("reads a measure's unit off its field", () => {
    expect(measureUnit(sessions, { op: "count", field: "" })).toBe("");
    expect(measureUnit(sessions, { op: "avg", field: "turn_count" })).toBe("");
    expect(
      measureUnit(sessions, { op: "p95", field: "duration_seconds" }),
    ).toBe("s");
  });
});

describe("measure and filter drafts", () => {
  it("names measures by their aggregation and field", () => {
    expect(measureAlias({ op: "count", field: "" })).toBe("count");
    expect(measureAlias({ op: "p95", field: "duration_seconds" })).toBe(
      "p95_duration_seconds",
    );
    expect(measureLabel({ op: "count", field: "" })).toBe("COUNT");
    expect(measureLabel({ op: "count_distinct", field: "user" })).toBe(
      "COUNT_DISTINCT(user)",
    );
    expect(measureLabel({ op: "avg", field: "turn_count" })).toBe(
      "AVG(turn_count)",
    );
  });

  it("drops measures still waiting on a field, and duplicates", () => {
    expect(
      completeMeasures([
        { op: "count", field: "" },
        { op: "sum", field: "" },
        { op: "sum", field: "turn_count" },
        { op: "sum", field: "turn_count" },
      ]),
    ).toEqual([
      { op: "count", field: "" },
      { op: "sum", field: "turn_count" },
    ]);
  });

  it("drops filters without a field or a value and normalizes the rest", () => {
    expect(
      completeFilters([
        { field: "", operator: "in", values: ["x"] },
        { field: "user", operator: "in", values: [" a ", "", "b", "a"] },
        { field: "surface", operator: "equals", values: ["cli", "web"] },
        { field: "surface", operator: "equals", values: [] },
      ]),
    ).toEqual([
      { field: "user", operator: "in", values: ["a", "b"] },
      { field: "surface", operator: "equals", values: ["cli"] },
    ]);
  });

  it("reads the limit control as a positive whole number, capped", () => {
    expect(parseLimit("")).toBe(0);
    expect(parseLimit("0")).toBe(0);
    expect(parseLimit("-5")).toBe(0);
    expect(parseLimit("2.5")).toBe(0);
    expect(parseLimit("50")).toBe(50);
    expect(parseLimit("5000")).toBe(1000);
    expect(parseLimit("5000", true)).toBe(200);
  });
});

describe("the queries a spec describes", () => {
  it("ends a window shorter than an hour on the next minute, not the next hour", () => {
    const now = Date.UTC(2026, 8, 14, 10, 17, 30);
    const { from, to } = windowRange("15m", now);
    expect(to.toISOString()).toBe("2026-09-14T10:18:00.000Z");
    expect(from.toISOString()).toBe("2026-09-14T10:03:00.000Z");
  });

  it("buckets every dashboard preset: hours up to three days, then days, then weeks", () => {
    expect(autoGrain("15m")).toBe("hour");
    expect(autoGrain("4h")).toBe("hour");
    expect(autoGrain("3d")).toBe("hour");
    expect(autoGrain("15d")).toBe("day");
    expect(autoGrain("90d")).toBe("week");
  });

  it("aligns the window to the hour so the key stays stable within it", () => {
    const now = Date.UTC(2026, 8, 14, 10, 17, 0);
    const { from, to } = windowRange("1d", now);
    expect(to.toISOString()).toBe("2026-09-14T11:00:00.000Z");
    expect(from.toISOString()).toBe("2026-09-13T11:00:00.000Z");
  });

  it("buckets a timeseries at the grain the window calls for, and other charts not at all", () => {
    expect(autoGrain("1h")).toBe("hour");
    expect(autoGrain("7d")).toBe("day");
    expect(autoGrain("90d")).toBe("week");
    const monthly = spec({ chartType: "line", window: "30d" });
    expect(queryBodyFromSpec(monthly).grain).toBe("day");
    expect(queryBodyFromSpec({ ...monthly, chartType: "table" }).grain).toBe(
      "none",
    );
  });

  it("draws a stack as the plain chart it is when there is nothing to stack by", () => {
    expect(isChartType("stacked_bar")).toBe(true);
    expect(isChartType("stacked_area")).toBe(true);
    expect(isStacked("stacked_area")).toBe(true);
    expect(isStacked("area")).toBe(false);

    const stacked = spec({ chartType: "stacked_bar", dimensions: ["user"] });
    expect(drawnChart(stacked)).toBe("stacked_bar");
    expect(drawnChart({ ...stacked, dimensions: [] })).toBe("bar");
    expect(
      drawnChart({ ...stacked, chartType: "stacked_area", dimensions: [] }),
    ).toBe("area");
    expect(drawnChart({ ...stacked, measures: [] })).toBe("table");
    expect(drawnChart(spec({ chartType: "number" }))).toBe("number");
  });

  it("draws a chart only for a timeseries over at least one measure", () => {
    expect(hasChartShape(spec({ chartType: "line" }))).toBe(true);
    expect(hasChartShape(spec({ chartType: "bar" }))).toBe(true);
    expect(hasChartShape(spec({ chartType: "stacked_bar" }))).toBe(true);
    expect(hasChartShape(spec({ chartType: "stacked_area" }))).toBe(true);
    expect(hasChartShape(spec({ chartType: "ranked" }))).toBe(false);
    expect(hasChartShape(spec({ chartType: "table" }))).toBe(false);
    expect(hasChartShape(spec({ chartType: "number" }))).toBe(false);
    expect(hasChartShape(spec({ chartType: "line", measures: [] }))).toBe(
      false,
    );
  });

  it("sends complete measures with their aliases and the filters that survived", () => {
    const body = queryBodyFromSpec(
      spec({
        measures: [
          { op: "count", field: "" },
          { op: "avg", field: "turn_count" },
          { op: "sum", field: "" },
        ],
        filters: [{ field: "surface", operator: "in", values: ["cli"] }],
        chartType: "table",
      }),
    );
    expect(body.dataset).toBe("sessions");
    expect(body.dimensions).toEqual(["user"]);
    expect(body.measures).toEqual([
      { op: "count", field: undefined, alias: "count" },
      { op: "avg", field: "turn_count", alias: "avg_turn_count" },
    ]);
    expect(body.filters).toEqual([
      { field: "surface", operator: "in", values: ["cli"] },
    ]);
    expect(body.ungrouped).toBeUndefined();
  });

  it("caps a timeseries at the server's maximum and leaves order to time", () => {
    const body = queryBodyFromSpec(
      spec({ chartType: "line", orderBy: "count", limit: 20 }),
    );
    expect(body.limit).toBe(1000);
    expect(body.orderBy).toBeUndefined();
  });

  it("drops the breakdown for a number chart", () => {
    expect(queryBodyFromSpec(spec({ chartType: "number" })).dimensions).toEqual(
      [],
    );
  });

  it("orders a whole-window result by a measure only while that measure is still in the query", () => {
    const ordered = spec({
      chartType: "table",
      measures: [{ op: "sum", field: "turn_count" }],
      orderBy: "sum_turn_count",
    });
    expect(queryBodyFromSpec(ordered).orderBy).toEqual([
      { measure: "sum_turn_count", direction: "desc" },
    ]);
    expect(
      queryBodyFromSpec({ ...ordered, measures: [{ op: "count", field: "" }] })
        .orderBy,
    ).toBeUndefined();
  });

  it("leaves a whole-window limit to the server unless one was typed", () => {
    const table = spec({ chartType: "table" });
    expect(queryBodyFromSpec(table).limit).toBeUndefined();
    expect(queryBodyFromSpec({ ...table, limit: 20 }).limit).toBe(20);
  });

  it("asks for rows at the dataset's grain when nothing is measured", () => {
    const rows = spec({ measures: [{ op: "sum", field: "" }] });
    expect(isRowsMode(rows)).toBe(true);
    const body = queryBodyFromSpec(rows);
    expect(body.ungrouped).toBe(true);
    expect(body.measures).toBeUndefined();
    expect(body.grain).toBe("none");
    expect(body.dimensions).toEqual(["user"]);
    expect(
      queryBodyFromSpec({ ...rows, limit: 500 }).limit,
      "a limit carried over from a grouped query is clamped to what rows allow",
    ).toBe(200);
  });
});

describe("formatting", () => {
  it("formats by unit", () => {
    expect(formatMeasureValue(1234.5, "usd")).toBe("$1,234.50");
    expect(formatMeasureValue(1234.5, "ms")).toBe("1,235 ms");
    expect(formatMeasureValue(90, "s")).toBe("90 s");
    expect(formatMeasureValue(0.256, "ratio")).toBe("25.6%");
    expect(formatMeasureValue(12_500, "")).toBe("12.5K");
  });

  it("reads result cells as numbers or text", () => {
    expect(numericCell(12)).toBe(12);
    expect(numericCell("12.5")).toBe(12.5);
    expect(numericCell(BigInt(7))).toBe(7);
    expect(numericCell("cli")).toBeNull();
    expect(numericCell(null)).toBeNull();
    expect(textCell("cli")).toBe("cli");
    expect(textCell("")).toBe("—");
    expect(textCell(undefined)).toBe("—");
    expect(textCell(3)).toBe("3");
    expect(textCell(true)).toBe("true");
  });
});

describe("filterForField", () => {
  it("starts a filter on a field with its first operator and no values", () => {
    const next = filterForField(sessions, "user");
    expect(next).toEqual({ field: "user", operator: "equals", values: [] });
  });
});

describe("longestWindow", () => {
  it("picks the longest window, reading an older spelling as today's", () => {
    expect(longestWindow(["7d", "90d", "1h"])).toBe("90d");
    expect(longestWindow(["24h", "4h"])).toBe("1d");
  });

  it("is undefined when nothing is a window", () => {
    expect(longestWindow([])).toBeUndefined();
    expect(longestWindow([undefined, "2h"])).toBeUndefined();
  });
});
