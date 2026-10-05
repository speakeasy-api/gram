import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import { describe, expect, it } from "vitest";
import {
  queryBodyFromSpec,
  specForDataset,
  type ExploreSpec,
} from "./exploreModel";
import { applyPageContext } from "./pageContext";

const toolCalls: AnalyticsDataset = {
  name: "tool_calls",
  kind: "event",
  grain: "tool call",
  description: "One row per tool call.",
  fields: [
    {
      name: "tool",
      type: "string",
      role: "dimension",
      default: true,
      operators: ["equals", "in"],
    },
    {
      name: "user",
      type: "string",
      role: "dimension",
      default: false,
      operators: ["in"],
    },
    {
      name: "model",
      type: "string",
      role: "dimension",
      default: false,
      operators: ["equals"],
    },
    {
      name: "duration_ms",
      type: "float64",
      role: "measure",
      default: false,
      unit: "ms",
      aggregations: ["avg"],
    },
  ],
};

function spec(overrides: Partial<ExploreSpec> = {}): ExploreSpec {
  return { ...specForDataset(toolCalls), window: "1d", ...overrides };
}

const preset = (value: "1d" | "30d" | "4h") => ({
  preset: value,
  customRange: null,
  customLabel: null,
});

describe("applyPageContext", () => {
  it("leaves a widget alone on no page", () => {
    const own = spec();
    expect(applyPageContext(own, toolCalls, undefined)).toEqual({
      spec: own,
      skipped: [],
      changed: false,
    });
    expect(applyPageContext(own, toolCalls, {}).changed).toBe(false);
  });

  it("replaces the widget's window with the page's preset, and the grain follows", () => {
    const { spec: paged, changed } = applyPageContext(
      spec({ chartType: "line" }),
      toolCalls,
      { window: preset("30d") },
    );
    expect(changed).toBe(true);
    expect(paged.window).toBe("30d");
    expect(queryBodyFromSpec(paged).grain).toBe("day");
  });

  it("takes any of the dashboard's presets, not only the builder's old five", () => {
    const { spec: paged } = applyPageContext(spec(), toolCalls, {
      window: preset("4h"),
    });
    const body = queryBodyFromSpec(paged);
    expect(body.to.getTime() - body.from.getTime()).toBe(4 * 3_600_000);
  });

  it("asks over the page's custom range, bucketed for its span, keeping its label", () => {
    const from = new Date(Date.UTC(2026, 8, 1));
    const to = new Date(Date.UTC(2026, 8, 29));
    const { spec: paged } = applyPageContext(
      spec({ chartType: "line" }),
      toolCalls,
      {
        window: {
          preset: null,
          customRange: { from, to },
          customLabel: "September",
        },
      },
    );
    expect(paged.range).toEqual({
      from: from.getTime(),
      to: to.getTime(),
      label: "September",
    });
    const body = queryBodyFromSpec(paged);
    expect(body.from).toEqual(from);
    expect(body.to).toEqual(to);
    expect(body.grain).toBe("day");
  });

  it("does not count the widget's own window as a change", () => {
    expect(
      applyPageContext(spec(), toolCalls, { window: preset("1d") }).changed,
    ).toBe(false);
  });

  it("ANDs page filters with the widget's own", () => {
    const own = { field: "tool", operator: "equals" as const, values: ["a"] };
    const { spec: paged, skipped } = applyPageContext(
      spec({ filters: [own] }),
      toolCalls,
      { filters: { user: ["ann"] } },
    );
    const added = { field: "user", operator: "in", values: ["ann"] };
    expect(paged.filters).toEqual([own, added]);
    expect(skipped).toEqual([]);
    expect(queryBodyFromSpec(paged).filters).toEqual([own, added]);
  });

  it("skips and names a page filter the dataset cannot apply", () => {
    const {
      spec: paged,
      skipped,
      changed,
    } = applyPageContext(spec(), toolCalls, {
      filters: {
        client: ["cursor"],
        // A dimension the dataset has, but not by `in`.
        model: ["sonnet"],
        // A measure is not a dimension a page can narrow by.
        duration_ms: ["1"],
      },
    });
    expect(paged.filters).toEqual([]);
    expect(skipped).toEqual(["client", "model", "duration_ms"]);
    expect(changed).toBe(false);
  });

  it("ignores a page filter with nothing picked", () => {
    const { skipped, changed } = applyPageContext(spec(), toolCalls, {
      filters: { client: [] },
    });
    expect(skipped).toEqual([]);
    expect(changed).toBe(false);
  });
});
