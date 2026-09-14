import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { describe, expect, it } from "vitest";
import {
  autoGrain,
  completeFilters,
  completeMeasures,
  fieldsForOp,
  filterableFields,
  formatMeasureValue,
  initialSpec,
  measureAlias,
  measureLabel,
  operatorsForField,
  opsForDataset,
  parseLimit,
  queryBodyFromSpec,
  specForDataset,
  windowRange,
  type ExploreSpec,
} from "./exploreModel";

const sessions: AnalyticsDataset = {
  name: "sessions",
  kind: "event",
  grain: "session",
  description: "One row per agent session.",
  summaryField: "user",
  fields: [
    {
      name: "user",
      type: "string",
      role: "dimension",
      operators: ["equals", "in"],
    },
    {
      name: "surface",
      type: "string",
      role: "dimension",
      operators: ["equals", "in"],
    },
    {
      name: "turn_count",
      type: "int64",
      role: "measure",
      aggregations: ["sum", "avg"],
    },
    {
      name: "duration_seconds",
      type: "float64",
      role: "measure",
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
    { name: "model", type: "string", role: "dimension", operators: ["in"] },
    {
      name: "tokens",
      type: "int64",
      role: "measure",
      aggregations: ["sum", "max"],
    },
  ],
};

function spec(overrides: Partial<ExploreSpec> = {}): ExploreSpec {
  return { ...specForDataset(sessions), ...overrides };
}

describe("the describe-to-controls mapping", () => {
  it("offers count plus every aggregation a measure field declares, in a fixed order", () => {
    expect(opsForDataset(sessions)).toEqual(["count", "sum", "avg", "p95"]);
    expect(opsForDataset(usage)).toEqual(["count", "sum", "max"]);
    expect(opsForDataset(undefined)).toEqual(["count"]);
  });

  it("targets an aggregation at the fields that admit it", () => {
    expect(fieldsForOp(sessions, "count")).toEqual([]);
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

  it("opens a dataset counting rows, broken down by its summary field, on the chart its kind picks", () => {
    expect(specForDataset(sessions)).toMatchObject({
      dataset: "sessions",
      measures: [{ op: "count", field: "" }],
      dimensions: ["user"],
      chartType: "line",
      window: "7d",
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
});

describe("measure and filter drafts", () => {
  it("names measures by their aggregation and field", () => {
    expect(measureAlias({ op: "count", field: "" })).toBe("count");
    expect(measureAlias({ op: "p95", field: "duration_seconds" })).toBe(
      "p95_duration_seconds",
    );
    expect(measureLabel({ op: "count", field: "" })).toBe("COUNT");
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
  });
});

describe("the query a spec describes", () => {
  it("aligns the window to the hour so the key stays stable within it", () => {
    const now = Date.UTC(2026, 8, 14, 10, 17, 0);
    const { from, to } = windowRange("24h", now);
    expect(to.toISOString()).toBe("2026-09-14T11:00:00.000Z");
    expect(from.toISOString()).toBe("2026-09-13T11:00:00.000Z");
  });

  it("buckets a timeseries at the grain the window calls for", () => {
    expect(autoGrain("1h")).toBe("hour");
    expect(autoGrain("7d")).toBe("day");
    expect(autoGrain("90d")).toBe("week");
    expect(
      queryBodyFromSpec(spec({ chartType: "line", window: "30d" })).grain,
    ).toBe("day");
    expect(queryBodyFromSpec(spec({ chartType: "table" })).grain).toBe("none");
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
  });

  it("drops the breakdown for a number chart", () => {
    expect(queryBodyFromSpec(spec({ chartType: "number" })).dimensions).toEqual(
      [],
    );
  });

  it("orders by a measure only while that measure is still in the query", () => {
    const ordered = spec({
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

  it("leaves the limit to the server unless one was typed", () => {
    expect(queryBodyFromSpec(spec()).limit).toBeUndefined();
    expect(queryBodyFromSpec(spec({ limit: 20 })).limit).toBe(20);
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
});
