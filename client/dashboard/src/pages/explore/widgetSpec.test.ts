import { describe, expect, it } from "vitest";
import type { ExploreSpec } from "./exploreModel";
import {
  differsFromWidget,
  specFromStoredWidget,
  specFromWidget,
  widgetFromSpec,
  widgetKey,
} from "./widgetSpec";

const spec: ExploreSpec = {
  dataset: "sessions",
  measures: [
    { op: "count", field: "" },
    { op: "sum", field: "turn_count" },
  ],
  filters: [{ field: "user", operator: "in", values: ["a", "b"] }],
  dimensions: ["user"],
  orderBy: "sum_turn_count",
  limit: 50,
  window: "7d",
  chartType: "line",
};

// What the server keeps is JSON, so a widget is read back from text.
function roundTrip(value: ExploreSpec): ExploreSpec | null {
  const stored = JSON.parse(JSON.stringify(widgetFromSpec(value))) as {
    query: Record<string, unknown>;
    visualization: Record<string, unknown>;
  };
  return specFromWidget(value.dataset, stored.query, stored.visualization);
}

describe("widgetFromSpec", () => {
  it("stores a timeseries as the one query it runs: bucketed, in time order, at the server's cap", () => {
    expect(widgetFromSpec(spec)).toEqual({
      query: {
        window: "7d",
        grain: "day",
        ungrouped: false,
        dimensions: ["user"],
        measures: [
          { op: "count", field: "", alias: "count" },
          { op: "sum", field: "turn_count", alias: "sum_turn_count" },
        ],
        filters: [{ field: "user", operator: "in", values: ["a", "b"] }],
        order_by: [],
        limit: 1000,
      },
      visualization: { type: "line", options: {} },
    });
  });

  it("keeps the order and limit of a whole-window chart", () => {
    const table = widgetFromSpec({ ...spec, chartType: "table" });
    expect(table.query).toMatchObject({
      grain: "none",
      order_by: [{ measure: "sum_turn_count", direction: "desc" }],
      limit: 50,
    });
  });

  it("draws rows as a table, whatever chart was picked", () => {
    const rows = widgetFromSpec({ ...spec, measures: [], orderBy: "" });
    expect(rows.query).toMatchObject({
      grain: "none",
      ungrouped: true,
      measures: [],
      order_by: [],
    });
    expect(rows.visualization).toEqual({ type: "table", options: {} });
  });

  it("never breaks a number tile down", () => {
    const tile = widgetFromSpec({ ...spec, chartType: "number" });
    expect(tile.query).toMatchObject({ grain: "none", dimensions: [] });
    expect(tile.visualization.type).toBe("number");
  });

  it("saves the dimensions the query ran with, even for rows left on Number", () => {
    const rows = widgetFromSpec({
      ...spec,
      measures: [],
      orderBy: "",
      chartType: "number",
    });
    expect(rows.query).toMatchObject({ ungrouped: true, dimensions: [] });
    expect(rows.visualization).toEqual({ type: "table", options: {} });
  });

  it("leaves out rows still being composed", () => {
    const saved = widgetFromSpec({
      ...spec,
      measures: [...spec.measures, { op: "avg", field: "" }],
      filters: [...spec.filters, { field: "", operator: "in", values: [] }],
    });
    expect(saved.query.measures).toHaveLength(2);
    expect(saved.query.filters).toHaveLength(1);
  });
});

describe("specFromWidget", () => {
  it.each<[string, ExploreSpec]>([
    ["a table", { ...spec, chartType: "table" }],
    ["rows", { ...spec, measures: [], orderBy: "", chartType: "table" }],
    ["a ranking", { ...spec, chartType: "ranked" }],
    ["the server's default limit", { ...spec, chartType: "table", limit: 0 }],
    ["a timeseries", { ...spec, orderBy: "", limit: 0 }],
    [
      "a distinct count over a dimension",
      {
        ...spec,
        chartType: "table",
        measures: [{ op: "count_distinct", field: "user" }],
        orderBy: "count_distinct_user",
      },
    ],
  ])("restores %s exactly as it was saved", (_, value) => {
    expect(roundTrip(value)).toEqual(value);
  });

  it("restores a timeseries with the builder's order and limit unset", () => {
    expect(roundTrip(spec)).toEqual({ ...spec, orderBy: "", limit: 0 });
  });

  it.each<[string, Record<string, unknown>]>([
    ["an order", { order_by: [{ measure: "count", direction: "desc" }] }],
    ["a limit below the cap", { limit: 20 }],
  ])("cannot read a timeseries stored with %s", (_, change) => {
    const stored = widgetFromSpec(spec);
    expect(
      specFromWidget(
        "sessions",
        { ...stored.query, ...change },
        stored.visualization,
      ),
    ).toBeNull();
  });

  it.each<[string, Record<string, unknown>, Record<string, unknown>]>([
    ["an unknown chart type", widgetFromSpec(spec).query, { type: "pie" }],
    [
      "an absolute window",
      { ...widgetFromSpec(spec).query, window: "2026-01" },
      { type: "line" },
    ],
    [
      "a malformed measure",
      { ...widgetFromSpec(spec).query, measures: [{}] },
      { type: "line" },
    ],
    [
      "an unknown aggregation",
      {
        ...widgetFromSpec(spec).query,
        measures: [{ op: "median", field: "turn_count" }],
      },
      { type: "line" },
    ],
    [
      "an ascending order",
      {
        ...widgetFromSpec(spec).query,
        order_by: [{ measure: "sum_turn_count", direction: "asc" }],
      },
      { type: "line" },
    ],
    [
      "a grain the builder would not pick",
      { ...widgetFromSpec(spec).query, grain: "hour" },
      { type: "line" },
    ],
    [
      "an unknown operator",
      {
        ...widgetFromSpec(spec).query,
        filters: [{ field: "user", operator: "like" }],
      },
      { type: "line" },
    ],
  ])("cannot read a widget with %s", (_, query, visualization) => {
    expect(specFromWidget("sessions", query, visualization)).toBeNull();
  });
});

describe("widgetKey and differsFromWidget", () => {
  it("treat builder states that save the same as the same widget", () => {
    const composing: ExploreSpec = {
      ...spec,
      measures: [...spec.measures, { op: "avg", field: "" }],
    };
    expect(widgetKey(composing)).toBe(widgetKey(spec));
    expect(widgetKey({ ...spec, window: "30d" })).not.toBe(widgetKey(spec));
  });

  it("see a rows widget as unchanged whatever chart the builder shows", () => {
    const rows: ExploreSpec = { ...spec, measures: [], orderBy: "" };
    const stored = { dataset: "sessions", ...widgetFromSpec(rows) };
    expect(differsFromWidget({ ...rows, chartType: "bar" }, stored)).toBe(
      false,
    );
    expect(differsFromWidget({ ...rows, window: "1d" }, stored)).toBe(true);
  });

  it("open a widget saved with the builder's old spelling of a day as 1d, unchanged", () => {
    const stored = {
      dataset: "sessions",
      ...widgetFromSpec({ ...spec, window: "1d" }),
    };
    const legacy = {
      ...stored,
      query: { ...stored.query, window: "24h" },
    };
    const opened = specFromStoredWidget(legacy);
    expect(opened?.window).toBe("1d");
    expect(differsFromWidget(opened!, legacy)).toBe(false);
  });
});
