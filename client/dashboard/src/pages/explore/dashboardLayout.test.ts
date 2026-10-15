import type { DashboardPlacement } from "@gram/client/models/components/dashboardplacement.js";
import { describe, expect, it } from "vitest";
import {
  layoutFor,
  placementsFor,
  sameLayout,
  type GridCard,
} from "./dashboardLayout";
import type { ViewableWidget } from "./WidgetView";

function widget(id: string, chartType: string): ViewableWidget {
  return {
    id,
    name: id,
    dataset: "sessions",
    query: {},
    visualization: { type: chartType },
  };
}

function placement(
  id: string,
  widgetId: string,
  box: [number, number, number, number],
): DashboardPlacement {
  const [x, y, w, h] = box;
  return { id, widgetId, x, y, w, h };
}

function card(
  id: string,
  chartType: string | undefined,
  box: [number, number, number, number],
): GridCard {
  return {
    placement: placement(id, chartType ?? "missing", box),
    widget: chartType === undefined ? undefined : widget(chartType, chartType),
  };
}

describe("layoutFor", () => {
  it("keys each card by its placement and holds it to its chart's minimum", () => {
    const layout = layoutFor([
      card("p-1", "number", [0, 0, 3, 2]),
      card("p-2", "line", [3, 0, 6, 3]),
      card("p-3", "number", [9, 0, 3, 2]),
    ]);
    expect(layout).toEqual([
      { i: "p-1", x: 0, y: 0, w: 3, h: 2, minW: 2, minH: 2 },
      { i: "p-2", x: 3, y: 0, w: 6, h: 3, minW: 4, minH: 3 },
      { i: "p-3", x: 9, y: 0, w: 3, h: 2, minW: 2, minH: 2 },
    ]);
  });

  it("treats a card whose widget has not loaded as a chart", () => {
    const [item] = layoutFor([card("p-1", undefined, [0, 0, 6, 3])]);
    expect(item).toMatchObject({ minW: 4, minH: 3 });
  });

  it("sizes a card that carries its own widget, linked to nothing, the same way", () => {
    const [item] = layoutFor([
      {
        placement: placement("built:0", "", [0, 0, 6, 2]),
        widget: {
          name: "Tool calls",
          dataset: "tool_calls",
          query: {},
          visualization: { type: "number" },
        },
      },
    ]);
    expect(item).toEqual({
      i: "built:0",
      x: 0,
      y: 0,
      w: 6,
      h: 2,
      minW: 2,
      minH: 2,
    });
  });
});

describe("placementsFor", () => {
  const saved = [
    placement("p-1", "number", [0, 0, 3, 2]),
    placement("p-2", "line", [3, 0, 6, 3]),
  ];

  it("saves every card where the grid put it, by its placement", () => {
    expect(
      placementsFor(
        [
          { i: "p-2", x: 0, y: 0, w: 8, h: 4 },
          { i: "p-1", x: 8, y: 0, w: 3, h: 2 },
        ],
        saved,
      ),
    ).toEqual([
      { id: "p-2", widgetId: "line", x: 0, y: 0, w: 8, h: 4 },
      { id: "p-1", widgetId: "number", x: 8, y: 0, w: 3, h: 2 },
    ]);
  });

  it("leaves out a grid item that is not a card", () => {
    expect(
      placementsFor([{ i: "stray", x: 0, y: 0, w: 1, h: 1 }], saved),
    ).toEqual([]);
  });
});

describe("sameLayout", () => {
  const saved = [placement("p-1", "number", [0, 0, 3, 2])];

  it("is true when every card is where it is saved", () => {
    expect(sameLayout([{ i: "p-1", x: 0, y: 0, w: 3, h: 2 }], saved)).toBe(
      true,
    );
  });

  it("is false once a card moves, resizes, or goes", () => {
    expect(sameLayout([{ i: "p-1", x: 1, y: 0, w: 3, h: 2 }], saved)).toBe(
      false,
    );
    expect(sameLayout([{ i: "p-1", x: 0, y: 0, w: 4, h: 2 }], saved)).toBe(
      false,
    );
    expect(sameLayout([], saved)).toBe(false);
  });
});
