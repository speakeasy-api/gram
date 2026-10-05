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
  return { ...specForDataset(toolCalls), window: "24h", ...overrides };
}

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

  it("replaces the widget's window with the page's, and the grain follows", () => {
    const { spec: paged, changed } = applyPageContext(
      spec({ chartType: "line" }),
      toolCalls,
      { window: "30d" },
    );
    expect(changed).toBe(true);
    expect(paged.window).toBe("30d");
    expect(queryBodyFromSpec(paged).grain).toBe("day");
  });

  it("asks over the page's absolute range, bucketed for its span", () => {
    const from = Date.UTC(2026, 8, 1);
    const to = Date.UTC(2026, 8, 29);
    const { spec: paged } = applyPageContext(
      spec({ chartType: "line" }),
      toolCalls,
      { window: { from, to } },
    );
    const body = queryBodyFromSpec(paged);
    expect(body.from.getTime()).toBe(from);
    expect(body.to.getTime()).toBe(to);
    expect(body.grain).toBe("day");
  });

  it("does not count the widget's own window as a change", () => {
    expect(applyPageContext(spec(), toolCalls, { window: "24h" }).changed).toBe(
      false,
    );
  });

  it("ANDs page filters with the widget's own", () => {
    const own = { field: "tool", operator: "equals" as const, values: ["a"] };
    const pageFilter = {
      field: "user",
      operator: "in" as const,
      values: ["ann"],
    };
    const { spec: paged, skipped } = applyPageContext(
      spec({ filters: [own] }),
      toolCalls,
      { filters: [pageFilter] },
    );
    expect(paged.filters).toEqual([own, pageFilter]);
    expect(skipped).toEqual([]);
    expect(queryBodyFromSpec(paged).filters).toEqual([own, pageFilter]);
  });

  it("skips and names a page filter the dataset cannot apply", () => {
    const {
      spec: paged,
      skipped,
      changed,
    } = applyPageContext(spec(), toolCalls, {
      filters: [
        { field: "client", operator: "in", values: ["cursor"] },
        // A field the dataset has, by an operator it does not admit.
        { field: "user", operator: "equals", values: ["ann"] },
        // A measure is not a dimension a page can narrow by.
        { field: "duration_ms", operator: "in", values: ["1"] },
      ],
    });
    expect(paged.filters).toEqual([]);
    expect(skipped).toEqual(["client", "user", "duration_ms"]);
    expect(changed).toBe(false);
  });

  it("ignores a page filter with nothing picked", () => {
    const { skipped, changed } = applyPageContext(spec(), toolCalls, {
      filters: [{ field: "client", operator: "in", values: [] }],
    });
    expect(skipped).toEqual([]);
    expect(changed).toBe(false);
  });
});
