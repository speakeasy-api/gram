import { describe, expect, it } from "vitest";
import type { ExploreSpec } from "./exploreModel";
import {
  savedSpecFromSpec,
  savedSpecKey,
  specFromSavedSpec,
} from "./savedSpec";

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

// What the server keeps is JSON, so a saved spec is read back from text.
function roundTrip(value: ExploreSpec): ExploreSpec | null {
  const stored = JSON.parse(JSON.stringify(savedSpecFromSpec(value))) as Record<
    string,
    unknown
  >;
  return specFromSavedSpec(value.dataset, stored);
}

describe("savedSpecFromSpec", () => {
  it("states the question in the shape the server plans", () => {
    expect(savedSpecFromSpec(spec)).toEqual({
      chart_type: "line",
      window: "7d",
      grain: "day",
      ungrouped: false,
      dimensions: ["user"],
      measures: [
        { op: "count", field: "", alias: "count" },
        { op: "sum", field: "turn_count", alias: "sum_turn_count" },
      ],
      filters: [{ field: "user", operator: "in", values: ["a", "b"] }],
      order_by: [{ measure: "sum_turn_count", direction: "desc" }],
      limit: 50,
    });
  });

  it("asks for rows when nothing is measured", () => {
    const rows = savedSpecFromSpec({ ...spec, measures: [], orderBy: "" });
    expect(rows).toMatchObject({
      grain: "none",
      ungrouped: true,
      measures: [],
      order_by: [],
    });
  });

  it("leaves out rows still being composed", () => {
    const saved = savedSpecFromSpec({
      ...spec,
      measures: [...spec.measures, { op: "avg", field: "" }],
      filters: [...spec.filters, { field: "", operator: "in", values: [] }],
    });
    expect(saved.measures).toHaveLength(2);
    expect(saved.filters).toHaveLength(1);
  });
});

describe("specFromSavedSpec", () => {
  it.each<[string, ExploreSpec]>([
    ["an aggregate", spec],
    ["rows", { ...spec, measures: [], orderBy: "", chartType: "table" }],
    ["a number tile", { ...spec, chartType: "number" }],
    ["the server's default limit", { ...spec, limit: 0 }],
  ])("restores %s exactly as it was saved", (_, value) => {
    expect(roundTrip(value)).toEqual(value);
  });

  it.each<[string, Record<string, unknown>]>([
    [
      "an unknown chart type",
      { ...savedSpecFromSpec(spec), chart_type: "pie" },
    ],
    ["an absolute window", { ...savedSpecFromSpec(spec), window: "2026-01" }],
    ["a malformed measure", { ...savedSpecFromSpec(spec), measures: [{}] }],
    [
      "an unknown operator",
      {
        ...savedSpecFromSpec(spec),
        filters: [{ field: "user", operator: "like" }],
      },
    ],
  ])("cannot read a spec with %s", (_, saved) => {
    expect(specFromSavedSpec("sessions", saved)).toBeNull();
  });
});

describe("savedSpecKey", () => {
  it("treats builder states that save the same as the same query", () => {
    const composing: ExploreSpec = {
      ...spec,
      measures: [...spec.measures, { op: "avg", field: "" }],
    };
    expect(savedSpecKey(composing)).toBe(savedSpecKey(spec));
    expect(savedSpecKey({ ...spec, window: "30d" })).not.toBe(
      savedSpecKey(spec),
    );
  });
});
